package chatdc

import (
	"encoding/json"
	"log/slog"
	"strings"
	"sync"

	"github.com/glimesh/broadcast-box/internal/chat"
	"github.com/glimesh/broadcast-box/internal/emotes"
	"github.com/pion/webrtc/v4"
	"golang.org/x/time/rate"
)

type Handler struct {
	manager *chat.Manager
}

func NewHandler(cm *chat.Manager) *Handler {
	return &Handler{manager: cm}
}

const DataChannelLabel = "bb-chat-v1"

// Chat messages per second (and burst) a single viewer may send
const (
	chatRateLimit = rate.Limit(1)
	chatRateBurst = 5
)

const (
	inboundTypeSend = "chat.send"

	outboundTypeConnected = "chat.connected"
	outboundTypeHistory   = "chat.history"
	outboundTypeMessage   = "chat.message"
	outboundTypeAck       = "chat.ack"
	outboundTypeError     = "chat.error"
)

type inboundMessage struct {
	Type          string            `json:"type"`
	ClientMessage string            `json:"clientMsgId,omitempty"`
	Text          string            `json:"text,omitempty"`
	DisplayName   string            `json:"displayName,omitempty"`
	Emotes        map[string]string `json:"emotes,omitempty"`
}

// Emotes a single message may carry
const maxMessageEmotes = 20

// Keeps emotes that are used in the text and hosted on a provider CDN
func validMessageEmotes(text string, requested map[string]string) map[string]string {
	if len(requested) == 0 {
		return nil
	}

	words := map[string]bool{}
	for word := range strings.FieldsSeq(text) {
		words[word] = true
	}

	valid := map[string]string{}
	for code, url := range requested {
		if len(valid) >= maxMessageEmotes {
			break
		}
		if words[code] && emotes.IsValidCode(code) && emotes.IsAllowedImageURL(url) {
			valid[code] = url
		}
	}
	if len(valid) == 0 {
		return nil
	}
	return valid
}

type outboundMessage struct {
	Type          string       `json:"type"`
	ClientMessage string       `json:"clientMsgId,omitempty"`
	Error         string       `json:"error,omitempty"`
	EventID       uint64       `json:"eventId,omitempty"`
	Message       chat.Message `json:"message,omitzero"`
	Events        []chat.Event `json:"events,omitempty"`
}

func (h *Handler) Bind(streamKey string, peerID string, dataChannel *webrtc.DataChannel) {
	if dataChannel.Label() != DataChannelLabel {
		return
	}

	if h.manager == nil {
		slog.Info("ChatDC.Bind: chat manager not configured")
		return
	}

	var (
		closeSubscription func()
		closeLock         sync.Mutex
		writeLock         sync.Mutex
		sendLimiter       = rate.NewLimiter(chatRateLimit, chatRateBurst)
	)
	closeSubscription = func() {}

	runCloseSubscription := func() {
		closeLock.Lock()
		defer closeLock.Unlock()
		closeSubscription()
	}

	send := func(payload outboundMessage) bool {
		data, err := json.Marshal(payload)
		if err != nil {
			slog.Error("ChatDC.Bind: marshal error", "err", err)
			return false
		}

		writeLock.Lock()
		defer writeLock.Unlock()

		if err := dataChannel.SendText(string(data)); err != nil {
			slog.Error("ChatDC.Bind: send error", "err", err)
			return false
		}

		return true
	}

	dataChannel.OnOpen(func() {
		slog.Info("ChatDC.Bind: open", "streamKey", streamKey, "peerID", peerID)

		ch, unsubscribe, history, err := h.manager.SubscribeStream(streamKey, 0)
		if err != nil {
			_ = send(outboundMessage{Type: outboundTypeError, Error: err.Error()})
			return
		}

		closeLock.Lock()
		closeSubscription = sync.OnceFunc(unsubscribe)
		closeLock.Unlock()
		if !send(outboundMessage{Type: outboundTypeConnected}) {
			runCloseSubscription()
			return
		}

		if len(history) > 0 {
			if !send(outboundMessage{Type: outboundTypeHistory, Events: history}) {
				runCloseSubscription()
				return
			}
		}

		go func() {
			for event := range ch {
				switch event.Type {
				case chat.EventTypeMessage:
					if !send(outboundMessage{Type: outboundTypeMessage, EventID: event.ID, Message: event.Message}) {
						runCloseSubscription()
						return
					}
				}
			}
		}()
	})

	dataChannel.OnMessage(func(msg webrtc.DataChannelMessage) {
		var inbound inboundMessage
		if err := json.Unmarshal(msg.Data, &inbound); err != nil {
			_ = send(outboundMessage{Type: outboundTypeError, Error: "invalid payload"})
			return
		}

		switch inbound.Type {
		case inboundTypeSend:
			text := strings.TrimSpace(inbound.Text)
			displayName := strings.TrimSpace(inbound.DisplayName)

			if len(text) < 1 || len(text) > 2000 {
				_ = send(outboundMessage{Type: outboundTypeError, Error: "invalid message length", ClientMessage: inbound.ClientMessage})
				return
			}

			if len(displayName) < 1 || len(displayName) > 80 {
				_ = send(outboundMessage{Type: outboundTypeError, Error: "invalid display name length", ClientMessage: inbound.ClientMessage})
				return
			}

			if !sendLimiter.Allow() {
				_ = send(outboundMessage{Type: outboundTypeError, Error: "you are sending messages too fast", ClientMessage: inbound.ClientMessage})
				return
			}

			if err := h.manager.SendToStream(streamKey, text, displayName, validMessageEmotes(text, inbound.Emotes)); err != nil {
				_ = send(outboundMessage{Type: outboundTypeError, Error: err.Error(), ClientMessage: inbound.ClientMessage})
				return
			}

			_ = send(outboundMessage{Type: outboundTypeAck, ClientMessage: inbound.ClientMessage})
		default:
			_ = send(outboundMessage{Type: outboundTypeError, Error: "unsupported message type"})
		}
	})

	dataChannel.OnClose(func() {
		slog.Info("ChatDC.Bind: closed", "streamKey", streamKey, "peerID", peerID)
		runCloseSubscription()
	})

	dataChannel.OnError(func(err error) {
		slog.Error("ChatDC.Bind: error", "streamKey", streamKey, "peerID", peerID, "err", err)
		runCloseSubscription()
	})
}
