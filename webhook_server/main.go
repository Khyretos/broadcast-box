package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ---------- config ----------

var (
	webhookURL      string
	publicBaseURL   string
	broadcastBoxURL string
	isDiscord       bool
)

const (
	colorLive  = 0xED4245 // red
	colorEnded = 0x95A5A6 // gray
)

// ---------- Broadcast Box webhook ----------

type webhookPayload struct {
	Action      string            `json:"action"`
	IP          string            `json:"ip"`
	BearerToken string            `json:"bearerToken"`
	QueryParams map[string]string `json:"queryParams"`
	UserAgent   string            `json:"userAgent"`
}

type webhookResponse struct {
	StreamKey string `json:"streamKey"`
}

// ---------- Discord-specific shapes ----------

type discordEmbed struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
	Color       int    `json:"color,omitempty"`
	Timestamp   string `json:"timestamp,omitempty"`
}
type discordPayload struct {
	Embeds []discordEmbed `json:"embeds,omitempty"`
}
type discordMessageResponse struct {
	ID string `json:"id"`
}

// ---------- Generic payload (works for Slack, Mattermost, n8n, etc.) ----------

type genericPayload struct {
	Content string `json:"content,omitempty"` // Discord
	Text    string `json:"text,omitempty"`    // Slack / Mattermost / Rocket.Chat
	Message string `json:"message,omitempty"` // many others
}

// ---------- active stream tracking ----------

type activeStream struct {
	StreamKey string
	MessageID string // only used for Discord
	StartedAt time.Time
	IP        string
}

var activeStreams sync.Map // streamKey -> *activeStream

// ---------- env helper ----------

// envOr reads an env var and strips surrounding whitespace and quotes.
// Solves the classic PUBLIC_URL="https://..." .env mistake.
func envOr(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	v = strings.TrimPrefix(v, "\"")
	v = strings.TrimSuffix(v, "\"")
	v = strings.TrimPrefix(v, "'")
	v = strings.TrimSuffix(v, "'")
	return strings.TrimSpace(v)
}

// ---------- helpers ----------

func parseStreamKeys(env string) map[string]string {
	m := make(map[string]string)
	if env == "" {
		return m
	}
	for _, pair := range strings.Split(env, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) == 2 {
			m[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return m
}

func buildStreamURL(streamKey string) string {
	if publicBaseURL == "" {
		return streamKey
	}
	return strings.TrimSuffix(publicBaseURL, "/") + "/" + streamKey
}

func isValidURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// ---------- sending ----------

// sendWebhook POSTs a JSON body. Returns a message ID (only meaningful for Discord).
func sendWebhook(body []byte) (string, error) {
	if webhookURL == "" {
		return "", fmt.Errorf("no webhook url")
	}
	url := webhookURL
	if isDiscord {
		if strings.Contains(url, "?") {
			url += "&wait=true"
		} else {
			url += "?wait=true"
		}
	}
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("webhook %d: %s", resp.StatusCode, b)
	}
	if isDiscord {
		var msg discordMessageResponse
		_ = json.NewDecoder(resp.Body).Decode(&msg)
		return msg.ID, nil
	}
	return "", nil
}

// editDiscordMessage patches a previously-sent Discord message. No-op otherwise.
func editDiscordMessage(messageID string, body []byte) error {
	if !isDiscord || messageID == "" {
		return fmt.Errorf("not a discord message")
	}
	url := strings.TrimSuffix(webhookURL, "/") + "/messages/" + messageID
	req, _ := http.NewRequest(http.MethodPatch, url, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord %d: %s", resp.StatusCode, b)
	}
	return nil
}

// ---------- notifications ----------

func sendStartNotification(streamKey string) string {
	if webhookURL == "" {
		return ""
	}
	streamURL := buildStreamURL(streamKey)

	var body []byte
	if isDiscord {
		embed := discordEmbed{
			Title:     "🔴 Stream is Live:",
			Color:     colorLive,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		if isValidURL(streamURL) {
			embed.Description = streamURL
			embed.URL = streamURL
		} else {
			embed.Description = streamKey
		}
		body, _ = json.Marshal(discordPayload{Embeds: []discordEmbed{embed}})
	} else {
		line := "🔴 Stream is Live:"
		if isValidURL(streamURL) {
			line += " " + streamURL
		} else {
			line += " " + streamKey
		}
		body, _ = json.Marshal(genericPayload{
			Content: line,
			Text:    line,
			Message: line,
		})
	}

	log.Printf("Webhook payload (start): %s", body)

	id, err := sendWebhook(body)
	if err != nil {
		log.Printf("Failed to send start notification: %v", err)
		return ""
	}
	log.Printf("✅ Start notification sent for %s (msg %s)", streamKey, id)
	return id
}

func sendStopNotification(s *activeStream) {
	if webhookURL == "" {
		return
	}
	duration := time.Since(s.StartedAt).Round(time.Second)
	streamURL := buildStreamURL(s.StreamKey)

	var body []byte
	if isDiscord {
		embed := discordEmbed{
			Title:     "⚫ Stream Ended:",
			Color:     colorEnded,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		line := streamURL
		if !isValidURL(streamURL) {
			line = s.StreamKey
		}
		embed.Description = fmt.Sprintf("%s\nDuration: %s", line, duration)
		if isValidURL(streamURL) {
			embed.URL = streamURL
		}
		body, _ = json.Marshal(discordPayload{Embeds: []discordEmbed{embed}})
	} else {
		line := "⚫ Stream Ended:"
		if isValidURL(streamURL) {
			line += " " + streamURL
		} else {
			line += " " + s.StreamKey
		}
		line += " (Duration: " + duration.String() + ")"
		body, _ = json.Marshal(genericPayload{
			Content: line,
			Text:    line,
			Message: line,
		})
	}

	log.Printf("Webhook payload (stop): %s", body)

	if isDiscord && s.MessageID != "" {
		if err := editDiscordMessage(s.MessageID, body); err == nil {
			log.Printf("✅ Marked stream %s as ended (edited msg)", s.StreamKey)
			return
		} else {
			log.Printf("Couldn't edit original message (%v), sending new one", err)
		}
	}
	if _, err := sendWebhook(body); err != nil {
		log.Printf("Failed to post stop notification: %v", err)
	}
}

// ---------- stream liveness poller ----------

func isStreamAlive(streamKey string) bool {
	if broadcastBoxURL == "" {
		return true
	}
	url := strings.TrimSuffix(broadcastBoxURL, "/") + "/api/status?key=" + streamKey

	req, _ := http.NewRequest(http.MethodGet, url, nil)
	// /api/status is public for public streams. If you ever need private streams,
	// switch to /api/admin/status and send the bearer token.
	// req.Header.Set("Authorization", "Bearer "+envOr("FRONTEND_ADMIN_TOKEN"))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("poll: request error for %q: %v", streamKey, err)
		return true
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	snippet := string(body)
	if len(snippet) > 400 {
		snippet = snippet[:400] + "…"
	}
	log.Printf("poll: HTTP %d for %q, body=%s", resp.StatusCode, streamKey, snippet)

	// 404 / 204 → stream is gone
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNoContent {
		return false
	}
	if resp.StatusCode != http.StatusOK {
		return true // fail safe on unexpected codes
	}

	trimmed := strings.TrimSpace(string(body))
	// Empty array / object / null / "{}" / "[]" → no active stream
	if trimmed == "" || trimmed == "null" || trimmed == "{}" || trimmed == "[]" {
		return false
	}

	// Any non-empty JSON body means the stream is active
	return true
}

func startPoller() {
	if broadcastBoxURL == "" {
		log.Println("⚠️ BROADCAST_BOX_URL not set – stream-end detection disabled")
		return
	}
	log.Println("🔎 Polling Broadcast Box every 30s:", broadcastBoxURL)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for range ticker.C {
			activeStreams.Range(func(k, v any) bool {
				s := v.(*activeStream)
				if !isStreamAlive(s.StreamKey) {
					log.Printf("Stream %s ended, sending notification", s.StreamKey)
					sendStopNotification(s)
					activeStreams.Delete(k)
				}
				return true
			})
		}
	}()
}

// ---------- HTTP handler ----------

func webhookHandler(tokenToStreamKey map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST is accepted", http.StatusMethodNotAllowed)
			return
		}
		var payload webhookPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")

		switch payload.Action {
			case "whip-connect":
				streamKey := payload.BearerToken
				if mapped, ok := tokenToStreamKey[payload.BearerToken]; ok {
					streamKey = mapped
				}
				log.Printf("Stream started: key=%s (IP %s)", streamKey, payload.IP)
				json.NewEncoder(w).Encode(webhookResponse{StreamKey: streamKey})
				go func(key, ip string) {
					msgID := sendStartNotification(key)
					activeStreams.Store(key, &activeStream{
						StreamKey: key,
						MessageID: msgID,
						StartedAt: time.Now(),
							    IP:        ip,
					})
				}(streamKey, payload.IP)

			case "whep-connect":
				log.Printf("WHEP request: key=%s (IP %s)", payload.BearerToken, payload.IP)
				json.NewEncoder(w).Encode(webhookResponse{StreamKey: payload.BearerToken})

			default:
				http.Error(w, "Invalid action", http.StatusBadRequest)
		}
	}
}

// ---------- main ----------

func main() {
	webhookURL = envOr("DISCORD_WEBHOOK_URL")
	publicBaseURL = envOr("PUBLIC_URL")
	broadcastBoxURL = envOr("BROADCAST_BOX_URL")
	isDiscord = strings.Contains(webhookURL, "discord.com") ||
	strings.Contains(webhookURL, "discordapp.com")

	log.Printf("🔧 webhookURL=%q", webhookURL)
	log.Printf("🔧 publicBaseURL=%q", publicBaseURL)
	log.Printf("🔧 broadcastBoxURL=%q", broadcastBoxURL)
	log.Printf("🔧 discord mode=%v", isDiscord)

	if webhookURL == "" {
		log.Println("⚠️ DISCORD_WEBHOOK_URL not set – notifications disabled")
	}
	if publicBaseURL == "" {
		log.Println("⚠️ PUBLIC_URL not set – will send stream key only")
	}

	tokenToStreamKey := parseStreamKeys(envOr("WEBHOOK_ENABLED_STREAMKEYS"))
	http.HandleFunc("/", webhookHandler(tokenToStreamKey))
	startPoller()

	log.Println("🚀 Webhook server listening on :8000")
	log.Fatal(http.ListenAndServe(":8000", nil))
}
