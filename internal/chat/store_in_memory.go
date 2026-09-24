package chat

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type subscriber struct {
	ch chan Event
}

type room struct {
	mu           sync.Mutex
	subscribers  map[string]*subscriber
	history      []Event
	nextEventID  uint64
	lastActivity time.Time
}

type InMemoryStore struct {
	mu         sync.RWMutex
	rooms      map[string]*room
	sessions   map[string]*Session
	maxHistory int

	// Social Stream Ninja forwarding (optional, disabled if SSN_SESSION_ID is unset)
	ssnURL     string
	ssnSession string
	ssnActive  bool
	ssnVerbose bool
	ssnMu      sync.Mutex
	ssnConn    *websocket.Conn
}

// envBool treats "1", "true", "yes", "on" (case-insensitive) as true.
func envBool(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	switch v {
		case "1", "true", "yes", "on":
			return true
	}
	return false
}

func NewInMemoryStore(maxHistory int) *InMemoryStore {
	if maxHistory <= 0 {
		maxHistory = DefaultMaxHistory
	}

	s := &InMemoryStore{
		rooms:      make(map[string]*room),
		sessions:   make(map[string]*Session),
		maxHistory: maxHistory,
	}

	if session := strings.TrimSpace(os.Getenv("SSN_SESSION_ID")); session != "" {
		// Root endpoint, matching what SSN's own Advanced Message Generator uses.
		// The session is carried inside the payload via the "apiid" field.
		s.ssnURL = fmt.Sprintf("wss://io.socialstream.ninja/join/%s/1/1", session)
		s.ssnSession = session
		s.ssnActive = true
		s.ssnVerbose = envBool("SSN_VERBOSE")
		go s.ssnWebSocketLoop()
		log.Printf("SSN: forwarding enabled, target=%s session=%s verbose=%v",
			   s.ssnURL, s.ssnSession, s.ssnVerbose)
	}

	return s
}

// ssnWebSocketLoop maintains a persistent WebSocket connection to SSN.
// Reconnects with a 30-second backoff if the connection drops.
func (s *InMemoryStore) ssnWebSocketLoop() {
	for {
		if !s.ssnActive {
			return
		}

		log.Printf("SSN: connecting to %s", s.ssnURL)
		conn, _, err := websocket.DefaultDialer.Dial(s.ssnURL, nil)
		if err != nil {
			log.Printf("SSN: connection failed: %v — retrying in 30s", err)
			time.Sleep(30 * time.Second)
			continue
		}

		s.ssnMu.Lock()
		s.ssnConn = conn
		s.ssnMu.Unlock()

		log.Println("SSN: connected")

		// Read loop — SSN may send control messages. We just drain them.
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				if s.ssnVerbose {
					log.Printf("SSN: read error: %v", err)
				}
				break
			}
		}

		s.ssnMu.Lock()
		s.ssnConn = nil
		s.ssnMu.Unlock()

		conn.Close()
		if s.ssnVerbose {
			log.Println("SSN: disconnected — reconnecting in 30s")
		}
		time.Sleep(30 * time.Second)
	}
}

// sendToSSN pushes an extContent message to SSN. Non-blocking: if the
// WebSocket isn't connected, the message is silently dropped.
func (s *InMemoryStore) sendToSSN(streamKey, displayName, text string) {
	s.ssnMu.Lock()
	conn := s.ssnConn
	s.ssnMu.Unlock()

	if conn == nil {
		if s.ssnVerbose {
			log.Printf("SSN: drop (not connected) stream=%q user=%q text=%q", streamKey, displayName, text)
		}
		return
	}

	// Inner content — same shape the SSN Advanced Message Generator produces.
	content := map[string]interface{}{
		"chatname":    displayName,
		"chatmessage": text,
		"type":        "api",
	}
	contentJSON, _ := json.Marshal(content)

	// Outer envelope — apiid carries the session ID, matching the generator.
	payload := map[string]interface{}{
		"action": "extContent",
		"value":  string(contentJSON),
	}
	payloadJSON, _ := json.Marshal(payload)

	if err := conn.WriteMessage(websocket.TextMessage, payloadJSON); err != nil {
		log.Printf("SSN: write failed (stream=%q user=%q): %v", streamKey, displayName, err)
		return
	}

	if s.ssnVerbose {
		log.Printf("SSN: sent stream=%q user=%q text=%q", streamKey, displayName, text)
	}
}

func (s *InMemoryStore) Connect(streamKey string, now time.Time) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	sessionID := uuid.New().String()
	s.sessions[sessionID] = &Session{
		ID:           sessionID,
		StreamKey:    streamKey,
		LastActivity: now,
	}

	s.getOrCreateRoomLocked(streamKey, now)

	return sessionID
}

func (s *InMemoryStore) GetSession(sessionID string, now time.Time) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, false
	}

	session.LastActivity = now
	copy := *session
	return &copy, true
}

func (s *InMemoryStore) TouchSession(sessionID string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[sessionID]
	if !ok {
		return false
	}

	session.LastActivity = now
	return true
}

func (s *InMemoryStore) Subscribe(sessionID string, lastEventID uint64, now time.Time) (chan Event, func(), []Event, error) {
	s.mu.Lock()
	session, ok := s.sessions[sessionID]
	if !ok {
		s.mu.Unlock()
		return nil, nil, nil, fmt.Errorf("invalid session")
	}

	session.LastActivity = now
	r, ok := s.rooms[session.StreamKey]
	s.mu.Unlock()

	if !ok {
		return nil, nil, nil, fmt.Errorf("room not found")
	}

	return s.subscribeToRoom(r, lastEventID, now)
}

func (s *InMemoryStore) SubscribeStream(streamKey string, lastEventID uint64, now time.Time) (chan Event, func(), []Event, error) {
	s.mu.Lock()
	r := s.getOrCreateRoomLocked(streamKey, now)
	s.mu.Unlock()

	return s.subscribeToRoom(r, lastEventID, now)
}

func (s *InMemoryStore) Send(sessionID string, text string, displayName string, now time.Time) error {
	s.mu.Lock()
	session, ok := s.sessions[sessionID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("invalid session")
	}

	session.LastActivity = now
	streamKey := session.StreamKey
	r, ok := s.rooms[streamKey]
	s.mu.Unlock()

	if !ok {
		return fmt.Errorf("room not found")
	}

	s.sendToRoom(streamKey, r, text, displayName, now)
	return nil
}

func (s *InMemoryStore) SendToStream(streamKey string, text string, displayName string, now time.Time) error {
	s.mu.Lock()
	r := s.getOrCreateRoomLocked(streamKey, now)
	s.mu.Unlock()

	s.sendToRoom(streamKey, r, text, displayName, now)
	return nil
}

func (s *InMemoryStore) Cleanup(now time.Time, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, session := range s.sessions {
		if now.Sub(session.LastActivity) > ttl {
			delete(s.sessions, id)
		}
	}

	for key, r := range s.rooms {
		r.mu.Lock()
		if len(r.subscribers) == 0 && now.Sub(r.lastActivity) > ttl {
			delete(s.rooms, key)
		}
		r.mu.Unlock()
	}
}

func (s *InMemoryStore) subscribeToRoom(r *room, lastEventID uint64, now time.Time) (chan Event, func(), []Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lastActivity = now
	subID := uuid.New().String()
	ch := make(chan Event, 100)
	r.subscribers[subID] = &subscriber{ch: ch}

	var history []Event
	if lastEventID > 0 {
		count := 0
		for _, ev := range r.history {
			if ev.ID > lastEventID {
				count++
			}
		}
		history = make([]Event, 0, count)
		for _, ev := range r.history {
			if ev.ID > lastEventID {
				history = append(history, ev)
			}
		}
	} else {
		history = make([]Event, len(r.history))
		copy(history, r.history)
	}

	cleanup := func() {
		r.mu.Lock()
		defer r.mu.Unlock()

		sub, ok := r.subscribers[subID]
		if !ok {
			return
		}

		delete(r.subscribers, subID)
		close(sub.ch)
	}

	return ch, cleanup, history, nil
}

func (s *InMemoryStore) sendToRoom(streamKey string, r *room, text string, displayName string, now time.Time) {
	r.mu.Lock()

	r.lastActivity = now
	event := Event{
		ID:   r.nextEventID,
		Type: EventTypeMessage,
		Message: Message{
			ID:          uuid.New().String(),
			TS:          now.UnixMilli(),
			Text:        text,
			DisplayName: displayName,
		},
	}
	r.nextEventID++

	if len(r.history) >= s.maxHistory {
		r.history = append(r.history[1:], event)
	} else {
		r.history = append(r.history, event)
	}

	for _, sub := range r.subscribers {
		select {
			case sub.ch <- event:
			default:
		}
	}

	r.mu.Unlock()

	// Forward to Social Stream Ninja, if configured.
	if s.ssnActive {
		s.sendToSSN(streamKey, displayName, text)
	}
}

func (s *InMemoryStore) getOrCreateRoomLocked(streamKey string, now time.Time) *room {
	r, ok := s.rooms[streamKey]
	if ok {
		r.lastActivity = now
		return r
	}

	r = &room{
		subscribers:  make(map[string]*subscriber),
		history:      make([]Event, 0, s.maxHistory),
		nextEventID:  1,
		lastActivity: now,
	}
	s.rooms[streamKey] = r

	return r
}
