package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type request struct {
	Method string
	Path   string
	Body   string
}

type fakeDiscord struct {
	mu       sync.Mutex
	requests []request
}

func (f *fakeDiscord) handler(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	f.mu.Lock()
	f.requests = append(f.requests, request{Method: r.Method, Path: r.URL.Path, Body: string(body)})
	f.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]string{"id": "123"})
}

func (f *fakeDiscord) snapshot() []request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]request(nil), f.requests...)
}

func newTestNotifier(t *testing.T, streamKeys []string) (*Notifier, *fakeDiscord) {
	fake := &fakeDiscord{}
	server := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(server.Close)

	return newNotifier(server.URL+"/api/webhooks/1/abc", "https://example.com/", 100*time.Millisecond, streamKeys, true), fake
}

func TestLiveThenEnded(t *testing.T) {
	n, fake := newTestNotifier(t, nil)

	n.Online("mykey")
	require.Eventually(t, func() bool { return len(fake.snapshot()) == 1 }, time.Second, 10*time.Millisecond)

	live := fake.snapshot()[0]
	require.Equal(t, http.MethodPost, live.Method)
	require.Contains(t, live.Body, "Stream is Live")
	require.Contains(t, live.Body, "https://example.com/mykey")

	n.Offline("mykey")
	require.Eventually(t, func() bool { return len(fake.snapshot()) == 2 }, time.Second, 10*time.Millisecond)

	ended := fake.snapshot()[1]
	require.Equal(t, http.MethodPatch, ended.Method)
	require.True(t, strings.HasSuffix(ended.Path, "/messages/123"), ended.Path)
	require.Contains(t, ended.Body, "Stream Ended")
	require.Contains(t, ended.Body, "Duration")
}

func TestReconnectWithinGracePeriodIsSilent(t *testing.T) {
	n, fake := newTestNotifier(t, nil)

	n.Online("mykey")
	require.Eventually(t, func() bool { return len(fake.snapshot()) == 1 }, time.Second, 10*time.Millisecond)

	n.Offline("mykey")
	n.Online("mykey")
	time.Sleep(300 * time.Millisecond)
	require.Len(t, fake.snapshot(), 1)

	// A later real disconnect still ends the stream
	n.Offline("mykey")
	require.Eventually(t, func() bool { return len(fake.snapshot()) == 2 }, time.Second, 10*time.Millisecond)
}

func TestStreamKeyFilter(t *testing.T) {
	n, fake := newTestNotifier(t, []string{"allowed"})

	n.Online("other")
	n.Online("allowed")
	require.Eventually(t, func() bool { return len(fake.snapshot()) == 1 }, time.Second, 10*time.Millisecond)
	require.Contains(t, fake.snapshot()[0].Body, "allowed")
}

func TestOfflineWithoutOnlineIsIgnored(t *testing.T) {
	n, fake := newTestNotifier(t, nil)

	n.Offline("mykey")
	time.Sleep(300 * time.Millisecond)
	require.Empty(t, fake.snapshot())
}
