package chat

import (
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/glimesh/broadcast-box/internal/environment"
	"github.com/gorilla/websocket"
)

const (
	ssnQueueSize        = 256
	ssnMaxMessageAge    = 2 * time.Minute
	ssnPingInterval     = 30 * time.Second
	ssnWriteTimeout     = 10 * time.Second
	ssnMinBackoff       = time.Second
	ssnMaxBackoff       = 30 * time.Second
	ssnStableConnection = 30 * time.Second
)

type ssnMessage struct {
	payload  []byte
	queuedAt time.Time
}

// ssnForwarder forwards chat messages to Social Stream Ninja
// (https://github.com/steveseguin/social_stream) as extContent messages.
//
// A single goroutine owns the WebSocket connection: it writes queued
// messages, sends keepalive pings and reconnects when the connection drops.
// Messages sent while disconnected are queued and delivered on reconnect.
type ssnForwarder struct {
	url        string
	verbose    bool
	streamKeys map[string]bool
	messages   chan ssnMessage
}

// newSSNForwarder returns nil when SSN_SESSION_ID is not set
func newSSNForwarder() *ssnForwarder {
	session := strings.TrimSpace(os.Getenv(environment.SSNSessionID))
	if session == "" {
		return nil
	}

	streamKeys := os.Getenv(environment.SSNStreamKeys)
	if streamKeys == "" {
		streamKeys = os.Getenv(environment.SSNStreamKeysLegacy)
	}

	f := &ssnForwarder{
		// Joins the session sending on channel 1, which the SSN extension/dock listens to
		url:        fmt.Sprintf("wss://io.socialstream.ninja/join/%s/1/1", session),
		verbose:    envBool(environment.SSNVerbose),
		streamKeys: map[string]bool{},
		messages:   make(chan ssnMessage, ssnQueueSize),
	}

	for key := range strings.SplitSeq(streamKeys, ",") {
		if key = strings.TrimSpace(key); key != "" {
			f.streamKeys[key] = true
		}
	}

	slog.Info("SSN: forwarding enabled", "session", session, "streamKeys", streamKeys, "verbose", f.verbose)
	go f.run()

	return f
}

// envBool treats "1", "true", "yes", "on" (case-insensitive) as true.
func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func (f *ssnForwarder) forward(streamKey, displayName, text string, emotes map[string]string) {
	if len(f.streamKeys) != 0 && !f.streamKeys[streamKey] {
		return
	}

	// Same shape the SSN Advanced Message Generator produces. Messages are
	// sent as text so viewers can't inject HTML into SSN overlays, unless they
	// contain emotes: then the text is escaped and emotes become images.
	chatMessage, textOnly := text, true
	if len(emotes) > 0 {
		chatMessage, textOnly = ssnMessageHTML(text, emotes), false
	}
	content, _ := json.Marshal(map[string]any{
		"chatname":    displayName,
		"chatmessage": chatMessage,
		"type":        "api",
		"sourceName":  streamKey,
		"textonly":    textOnly,
	})
	payload, _ := json.Marshal(map[string]any{
		"action": "extContent",
		"value":  string(content),
	})

	select {
	case f.messages <- ssnMessage{payload: payload, queuedAt: time.Now()}:
		if f.verbose {
			slog.Info("SSN: queued", "streamKey", streamKey, "user", displayName, "text", text)
		}
	default:
		slog.Error("SSN: queue full, dropping message", "streamKey", streamKey, "user", displayName)
	}
}

func ssnMessageHTML(text string, emotes map[string]string) string {
	var out strings.Builder
	for i, word := range strings.Split(text, " ") {
		if i > 0 {
			out.WriteByte(' ')
		}
		if url, ok := emotes[word]; ok {
			out.WriteString(`<img src="` + html.EscapeString(url) + `" alt="` + html.EscapeString(word) + `" class="regular-emote">`)
		} else {
			out.WriteString(html.EscapeString(word))
		}
	}
	return out.String()
}

func (f *ssnForwarder) run() {
	backoff := ssnMinBackoff
	var pending *ssnMessage

	for {
		slog.Info("SSN: connecting")
		conn, _, err := websocket.DefaultDialer.Dial(f.url, nil)
		if err != nil {
			slog.Error("SSN: connection failed", "err", err, "retryIn", backoff)
			time.Sleep(backoff)
			backoff = min(backoff*2, ssnMaxBackoff)
			continue
		}

		slog.Info("SSN: connected")
		connectedAt := time.Now()
		pending, err = f.serve(conn, pending)
		_ = conn.Close()

		// Only reset the backoff if the connection was usable for a while, so a
		// server that accepts and immediately drops us isn't hammered.
		if time.Since(connectedAt) > ssnStableConnection {
			backoff = ssnMinBackoff
		}

		slog.Info("SSN: disconnected", "err", err, "reconnectIn", backoff)
		time.Sleep(backoff)
		backoff = min(backoff*2, ssnMaxBackoff)
	}
}

// serve writes messages and pings until the connection fails. It returns the
// message that was being written when the connection failed, so it can be
// retried after reconnecting.
func (f *ssnForwarder) serve(conn *websocket.Conn, pending *ssnMessage) (*ssnMessage, error) {
	// SSN may send control messages, drain them so pongs and close frames are handled
	readErr := make(chan error, 1)
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				readErr <- err
				return
			}
		}
	}()

	pingTicker := time.NewTicker(ssnPingInterval)
	defer pingTicker.Stop()

	for {
		if pending != nil {
			if time.Since(pending.queuedAt) <= ssnMaxMessageAge {
				_ = conn.SetWriteDeadline(time.Now().Add(ssnWriteTimeout))
				if err := conn.WriteMessage(websocket.TextMessage, pending.payload); err != nil {
					return pending, err
				}
				if f.verbose {
					slog.Info("SSN: sent", "payload", string(pending.payload))
				}
			} else {
				slog.Info("SSN: dropping message older than max age", "maxAge", ssnMaxMessageAge)
			}
			pending = nil
		}

		select {
		case message := <-f.messages:
			pending = &message
		case <-pingTicker.C:
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(ssnWriteTimeout)); err != nil {
				return nil, err
			}
		case err := <-readErr:
			return nil, err
		}
	}
}
