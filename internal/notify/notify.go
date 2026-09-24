// Package notify posts "stream is live" / "stream ended" messages to a webhook
// (Discord, or any service accepting a JSON content/text/message payload).
//
// Events come straight from the WHIP session lifecycle, so there is no polling.
// A stream going offline is only announced after a grace period, so a short
// encoder reconnect keeps the original "live" message instead of spamming the
// channel with ended/live pairs.
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/glimesh/broadcast-box/internal/environment"
)

const (
	colorLive  = 0xED4245 // red
	colorEnded = 0x95A5A6 // gray

	defaultOfflineGracePeriod = 60 * time.Second
	eventQueueSize            = 256
)

type eventKind int

const (
	eventOnline eventKind = iota
	eventOffline
	eventOfflineConfirmed
)

type event struct {
	kind       eventKind
	streamKey  string
	at         time.Time
	generation uint64
}

type liveStream struct {
	messageID  string
	startedAt  time.Time
	offlineAt  time.Time
	timer      *time.Timer
	generation uint64
}

type Notifier struct {
	webhookURL  string
	publicURL   string
	isDiscord   bool
	gracePeriod time.Duration
	streamKeys  map[string]bool

	client *http.Client
	events chan event

	// Only accessed from the run goroutine
	streams map[string]*liveStream
}

var defaultNotifier *Notifier

// Setup reads the notification settings from the environment and starts the
// notifier. Notifications stay disabled when DISCORD_WEBHOOK_URL is not set.
func Setup() {
	webhookURL := envValue(environment.DiscordWebhookURL)
	if webhookURL == "" {
		slog.Info("Notify: DISCORD_WEBHOOK_URL not set, stream notifications disabled")
		return
	}

	gracePeriod := defaultOfflineGracePeriod
	if value := envValue(environment.NotifyOfflineGracePeriod); value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			slog.Error("Notify: invalid NOTIFY_OFFLINE_GRACE_PERIOD, using default", "value", value, "err", err)
		} else {
			gracePeriod = parsed
		}
	}

	defaultNotifier = New(webhookURL, envValue(environment.PublicURL), gracePeriod, parseList(envValue(environment.NotifyStreamKeys)))
	slog.Info("Notify: stream notifications enabled",
		"discord", defaultNotifier.isDiscord,
		"publicURL", defaultNotifier.publicURL,
		"offlineGracePeriod", gracePeriod)
}

func New(webhookURL, publicURL string, gracePeriod time.Duration, streamKeys []string) *Notifier {
	isDiscord := strings.Contains(webhookURL, "discord.com") || strings.Contains(webhookURL, "discordapp.com")
	return newNotifier(webhookURL, publicURL, gracePeriod, streamKeys, isDiscord)
}

func newNotifier(webhookURL, publicURL string, gracePeriod time.Duration, streamKeys []string, isDiscord bool) *Notifier {
	n := &Notifier{
		webhookURL:  webhookURL,
		publicURL:   strings.TrimSuffix(publicURL, "/"),
		isDiscord:   isDiscord,
		gracePeriod: gracePeriod,
		streamKeys:  map[string]bool{},
		client:      &http.Client{Timeout: 10 * time.Second},
		events:      make(chan event, eventQueueSize),
		streams:     map[string]*liveStream{},
	}

	for _, key := range streamKeys {
		n.streamKeys[key] = true
	}

	go n.run()
	return n
}

// StreamOnline is called when a publisher has connected to a stream
func StreamOnline(streamKey string) {
	if defaultNotifier != nil {
		defaultNotifier.Online(streamKey)
	}
}

// StreamOffline is called when a publisher has left a stream
func StreamOffline(streamKey string) {
	if defaultNotifier != nil {
		defaultNotifier.Offline(streamKey)
	}
}

func (n *Notifier) Online(streamKey string)  { n.enqueue(eventOnline, streamKey) }
func (n *Notifier) Offline(streamKey string) { n.enqueue(eventOffline, streamKey) }

func (n *Notifier) enqueue(kind eventKind, streamKey string) {
	if len(n.streamKeys) != 0 && !n.streamKeys[streamKey] {
		return
	}

	select {
	case n.events <- event{kind: kind, streamKey: streamKey, at: time.Now()}:
	default:
		slog.Error("Notify: event queue full, dropping event", "streamKey", streamKey)
	}
}

// All state changes and webhook calls happen on this goroutine, which keeps
// the live message and its later edit in order.
func (n *Notifier) run() {
	for e := range n.events {
		switch e.kind {
		case eventOnline:
			n.handleOnline(e)
		case eventOffline:
			n.handleOffline(e)
		case eventOfflineConfirmed:
			n.handleOfflineConfirmed(e)
		}
	}
}

func (n *Notifier) handleOnline(e event) {
	if stream, ok := n.streams[e.streamKey]; ok {
		if stream.timer != nil {
			stream.timer.Stop()
			stream.timer = nil
			stream.generation++
			slog.Info("Notify: stream reconnected within grace period", "streamKey", e.streamKey)
		}
		return
	}

	stream := &liveStream{startedAt: e.at}
	n.streams[e.streamKey] = stream

	messageID, err := n.send(n.liveMessage(e.streamKey))
	if err != nil {
		slog.Error("Notify: failed to send live notification", "streamKey", e.streamKey, "err", err)
		return
	}

	stream.messageID = messageID
	slog.Info("Notify: live notification sent", "streamKey", e.streamKey)
}

func (n *Notifier) handleOffline(e event) {
	stream, ok := n.streams[e.streamKey]
	if !ok || stream.timer != nil {
		return
	}

	stream.offlineAt = e.at
	stream.generation++
	generation := stream.generation
	stream.timer = time.AfterFunc(n.gracePeriod, func() {
		n.events <- event{kind: eventOfflineConfirmed, streamKey: e.streamKey, generation: generation}
	})
}

func (n *Notifier) handleOfflineConfirmed(e event) {
	stream, ok := n.streams[e.streamKey]
	if !ok || stream.generation != e.generation {
		return
	}
	delete(n.streams, e.streamKey)

	body := n.endedMessage(e.streamKey, stream.offlineAt.Sub(stream.startedAt))

	if n.isDiscord && stream.messageID != "" {
		err := n.editDiscordMessage(stream.messageID, body)
		if err == nil {
			slog.Info("Notify: marked stream as ended", "streamKey", e.streamKey)
			return
		}
		slog.Error("Notify: could not edit live message, sending a new one", "err", err)
	}

	if _, err := n.send(body); err != nil {
		slog.Error("Notify: failed to send ended notification", "streamKey", e.streamKey, "err", err)
	}
}

// Messages

type discordEmbed struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
	Color       int    `json:"color,omitempty"`
	Timestamp   string `json:"timestamp,omitempty"`
}

type discordPayload struct {
	Embeds []discordEmbed `json:"embeds"`
}

// Generic payload for Slack, Mattermost, n8n and similar services
type genericPayload struct {
	Content string `json:"content"`
	Text    string `json:"text"`
	Message string `json:"message"`
}

func (n *Notifier) streamURL(streamKey string) string {
	if n.publicURL == "" {
		return ""
	}
	return n.publicURL + "/" + streamKey
}

func (n *Notifier) liveMessage(streamKey string) []byte {
	return n.message("🔴 Stream is Live:", streamKey, "", colorLive)
}

func (n *Notifier) endedMessage(streamKey string, duration time.Duration) []byte {
	return n.message("⚫ Stream Ended:", streamKey, "Duration: "+duration.Round(time.Second).String(), colorEnded)
}

func (n *Notifier) message(title, streamKey, detail string, color int) []byte {
	target := n.streamURL(streamKey)
	if target == "" {
		target = streamKey
	}

	var body []byte
	if n.isDiscord {
		embed := discordEmbed{
			Title:       title,
			Description: target,
			URL:         n.streamURL(streamKey),
			Color:       color,
			Timestamp:   time.Now().UTC().Format(time.RFC3339),
		}
		if detail != "" {
			embed.Description += "\n" + detail
		}
		body, _ = json.Marshal(discordPayload{Embeds: []discordEmbed{embed}})
	} else {
		line := title + " " + target
		if detail != "" {
			line += " (" + detail + ")"
		}
		body, _ = json.Marshal(genericPayload{Content: line, Text: line, Message: line})
	}

	return body
}

// Transport

// send POSTs the body to the webhook. For Discord it returns the message ID so
// the message can later be edited.
func (n *Notifier) send(body []byte) (string, error) {
	url := n.webhookURL
	if n.isDiscord {
		if strings.Contains(url, "?") {
			url += "&wait=true"
		} else {
			url += "?wait=true"
		}
	}

	resp, err := n.do(http.MethodPost, url, body)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if !n.isDiscord {
		return "", nil
	}

	var message struct {
		ID string `json:"id"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&message)
	return message.ID, nil
}

func (n *Notifier) editDiscordMessage(messageID string, body []byte) error {
	base, query, _ := strings.Cut(n.webhookURL, "?")
	url := strings.TrimSuffix(base, "/") + "/messages/" + messageID
	if query != "" {
		url += "?" + query
	}

	resp, err := n.do(http.MethodPatch, url, body)
	if err != nil {
		return err
	}
	return resp.Body.Close()
}

func (n *Notifier) do(method, url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 300 {
		responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		_ = resp.Body.Close()
		return nil, fmt.Errorf("webhook returned %d: %s", resp.StatusCode, responseBody)
	}

	return resp, nil
}

// Helpers

// envValue reads an environment variable and strips surrounding whitespace and
// quotes, which are easy to leave in by accident in .env files.
func envValue(key string) string {
	value := strings.TrimSpace(os.Getenv(key))
	value = strings.Trim(value, `"'`)
	return strings.TrimSpace(value)
}

func parseList(value string) (result []string) {
	for item := range strings.SplitSeq(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return
}
