package chat

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestSSNForwarderDeliversAcrossReconnects(t *testing.T) {
	var (
		mu          sync.Mutex
		received    []string
		connections int
	)

	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		defer conn.Close()

		mu.Lock()
		connections++
		first := connections == 1
		mu.Unlock()

		// Drop the first connection straight away, like an idle timeout would
		if first {
			return
		}

		for {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}

			var envelope struct {
				Action string `json:"action"`
				Value  string `json:"value"`
			}
			require.NoError(t, json.Unmarshal(payload, &envelope))
			require.Equal(t, "extContent", envelope.Action)

			var content map[string]any
			require.NoError(t, json.Unmarshal([]byte(envelope.Value), &content))
			require.Equal(t, true, content["textonly"])

			mu.Lock()
			received = append(received, content["chatmessage"].(string))
			mu.Unlock()
		}
	}))
	defer server.Close()

	f := &ssnForwarder{
		url:        "ws" + strings.TrimPrefix(server.URL, "http"),
		streamKeys: map[string]bool{"mine": true},
		messages:   make(chan ssnMessage, ssnQueueSize),
	}
	go f.run()

	// Queue messages while the first connection is down
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return connections == 1
	}, 5*time.Second, 10*time.Millisecond)
	time.Sleep(200 * time.Millisecond)

	f.forward("mine", "viewer", "first", nil)
	f.forward("someone-else", "viewer", "ignored", nil)
	f.forward("mine", "viewer", "second", nil)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(received) == 2
	}, 10*time.Second, 50*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"first", "second"}, received)
	require.GreaterOrEqual(t, connections, 2)
}

func TestSSNMessageHTML(t *testing.T) {
	require.Equal(t,
		`hi <img src="https://cdn.7tv.app/emote/1/1x.webp" alt="catJAM" class="regular-emote"> &lt;b&gt;bold&lt;/b&gt;`,
		ssnMessageHTML("hi catJAM <b>bold</b>", map[string]string{"catJAM": "https://cdn.7tv.app/emote/1/1x.webp"}))
}
