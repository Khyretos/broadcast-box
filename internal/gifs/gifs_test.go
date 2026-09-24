package gifs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMatchesHost(t *testing.T) {
	require.True(t, MatchesHost("gifs.kreative-kompas.com", "gifs.kreative-kompas.com"))
	require.True(t, MatchesHost("GIFS.kreative-kompas.com", "gifs.Kreative-Kompas.com"))
	require.False(t, MatchesHost("gifs.kreative-kompas.com", "evil.kreative-kompas.com"))

	require.True(t, MatchesHost("*.giphy.com", "media.giphy.com"))
	require.True(t, MatchesHost("*.giphy.com", "media4.giphy.com"))
	require.True(t, MatchesHost("*.giphy.com", "giphy.com"))
	require.False(t, MatchesHost("*.giphy.com", "evilgiphy.com"))
	require.False(t, MatchesHost("*.giphy.com", "giphy.com.evil.com"))

	require.True(t, MatchesHost("*", "anything.example"))
}

func TestIsAllowedURL(t *testing.T) {
	hosts := []string{"gifs.kreative-kompas.com", "*.giphy.com"}
	require.True(t, IsAllowedURL("https://media2.giphy.com/media/abc/giphy.gif", hosts))
	require.True(t, IsAllowedURL("https://gifs.kreative-kompas.com/api/image/public/x.gif", hosts))
	require.False(t, IsAllowedURL("http://media.giphy.com/media/abc/giphy.gif", hosts), "https only")
	require.False(t, IsAllowedURL("https://media.giphy.com.evil.com/x.gif", hosts))
	require.False(t, IsAllowedURL("javascript:alert(1)", hosts))
}

func TestParseSlinkInstances(t *testing.T) {
	instances := ParseSlinkInstances(" gifs.kreative-kompas.com , https://other.example.com/ ,random.example.com:sk_secret, http://insecure.example.com")
	require.Equal(t, []slinkInstance{
		{baseURL: "https://gifs.kreative-kompas.com", host: "gifs.kreative-kompas.com"},
		{baseURL: "https://other.example.com", host: "other.example.com"},
		{baseURL: "https://random.example.com", host: "random.example.com"},
	}, instances)
}

// Response of Slink's GET /api/images, trimmed to the fields that matter
const slinkResponse = `{"meta":{"size":30,"total":3},"data":[
	{"id":"6131d2b6","owner":{"id":"u1","displayName":"Khyretos"},"url":"/api/image/public/6131d2b6.gif",
	 "attributes":{"fileName":"6131d2b6.gif","description":"cat dance","isPublic":true,"createdAt":"2026-09-01T10:00:00+00:00","views":4},
	 "metadata":{"size":12345,"mimeType":"image/gif","width":320,"height":240},"bookmarkCount":0},
	{"id":"a1","url":"/api/image/public/a1.png",
	 "attributes":{"fileName":"meme.png","description":"","isPublic":true},
	 "metadata":{"size":1,"mimeType":"image/png","width":10,"height":10}},
	{"id":"v1","url":"/api/image/public/v1.mp4",
	 "attributes":{"fileName":"clip.mp4","description":"video","isPublic":true},
	 "metadata":{"size":1,"mimeType":"video/mp4","width":10,"height":10}}
]}`

func newTestService(t *testing.T, options Options) (*Service, map[string]int, *[]string) {
	var lock sync.Mutex
	requests := map[string]int{}
	var queries []string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		defer lock.Unlock()
		requests[r.URL.Path]++
		switch r.URL.Path {
		case "/api/images":
			require.Equal(t, "30", r.URL.Query().Get("limit"))
			require.Empty(t, r.Header.Get("Authorization"), "public listing needs no key")
			queries = append(queries, r.URL.Query().Get("searchTerm"))
			_, _ = w.Write([]byte(slinkResponse))
		case "/v1/gifs/search":
			require.Equal(t, "giphy-key", r.URL.Query().Get("api_key"))
			require.Equal(t, "pg-13", r.URL.Query().Get("rating"))
			_, _ = w.Write([]byte(`{"data":[
				{"title":"Cat Dance GIF","images":{
					"fixed_height":{"url":"https://media3.giphy.com/media/abc/200.gif?cid=track","width":"356","height":"200"},
					"fixed_height_small":{"url":"https://media3.giphy.com/media/abc/100.gif?cid=track"}}},
				{"title":"Evil","images":{"fixed_height":{"url":"https://evil.example.com/x.gif"}}}
			]}`))
		case "/v2/search":
			// Tenor v2 format
			require.Equal(t, "klipy-key", r.URL.Query().Get("key"))
			require.Equal(t, "low", r.URL.Query().Get("contentfilter"))
			_, _ = w.Write([]byte(`{"results":[
				{"id":"1","title":"","content_description":"Cat Jam","media_formats":{
					"gif":{"url":"https://static.klipy.com/ii/abc/1c/6c/full.gif","dims":[498,280],"size":2000000},
					"mediumgif":{"url":"https://static.klipy.com/ii/abc/1c/6c/medium.gif","dims":[320,180],"size":500000},
					"tinygif":{"url":"https://static.klipy.com/ii/abc/1c/6c/tiny.gif","dims":[220,124],"size":90000}}},
				{"id":"2","title":"Only full","media_formats":{
					"gif":{"url":"https://static.klipy.com/ii/def/full.gif","dims":[200,100]}}},
				{"id":"3","title":"Elsewhere","media_formats":{"gif":{"url":"https://klipy.com/gifs/elsewhere"}}}
			],"next":"30"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	host := strings.TrimPrefix(server.URL, "https://")
	options.Slink = []slinkInstance{{baseURL: server.URL, host: strings.Split(host, ":")[0]}}
	service := New(options)
	service.client = server.Client()
	for _, provider := range service.apis {
		provider.baseURL = server.URL
	}
	return service, requests, &queries
}

func TestSlinkSearch(t *testing.T) {
	service, requests, queries := newTestService(t, Options{})
	result := service.Search(context.Background(), "cat", false)

	base := service.slink[0].baseURL
	require.Equal(t, []GIF{
		{URL: base + "/api/image/public/6131d2b6.gif", Preview: base + "/api/image/public/6131d2b6.gif", Width: 320, Height: 240, Title: "cat dance", Provider: ProviderSlink, Source: service.slink[0].host},
		{URL: base + "/api/image/public/a1.png", Preview: base + "/api/image/public/a1.png", Width: 10, Height: 10, Title: "meme", Provider: ProviderSlink, Source: service.slink[0].host},
	}, result.GIFs, "videos are skipped, file name is the fallback title")
	require.Equal(t, []string{"cat"}, *queries)

	// An empty query lists the newest images
	service.Search(context.Background(), "", false)
	require.Equal(t, []string{"cat", ""}, *queries)

	service.Search(context.Background(), "CAT", false)
	require.Equal(t, 2, requests["/api/images"], "cached")
}

func TestGiphyOnlyWhenRequested(t *testing.T) {
	service, requests, _ := newTestService(t, Options{GiphyKey: "giphy-key"})

	service.Search(context.Background(), "cat", false)
	require.Zero(t, requests["/v1/gifs/search"], "typing only searches Slink")

	result := service.Search(context.Background(), "cat", true)
	require.Equal(t, 1, requests["/v1/gifs/search"])
	require.Len(t, result.GIFs, 3)
	require.Equal(t, GIF{
		URL: "https://media3.giphy.com/media/abc/200.gif", Preview: "https://media3.giphy.com/media/abc/100.gif",
		Width: 356, Height: 200, Title: "Cat Dance GIF", Provider: ProviderGiphy, Source: "giphy.com",
	}, result.GIFs[1], "sources are interleaved")

	service.Search(context.Background(), "Cat", true)
	require.Equal(t, 1, requests["/v1/gifs/search"], "Giphy results are cached")

	service.Search(context.Background(), "", true)
	require.Equal(t, 1, requests["/v1/gifs/search"], "Giphy is never searched without a query")

	require.Equal(t, []string{service.slink[0].host, "*.giphy.com"}, service.Hosts())
}

func TestGiphyHourlyBudget(t *testing.T) {
	service, requests, _ := newTestService(t, Options{GiphyKey: "giphy-key", GiphyPerHour: 2})
	now := time.Now()
	service.now = func() time.Time { return now }

	service.Search(context.Background(), "one", true)
	service.Search(context.Background(), "two", true)
	result := service.Search(context.Background(), "three", true)
	require.Equal(t, 2, requests["/v1/gifs/search"])
	require.Equal(t, []string{ProviderGiphy}, result.Limited)
	require.Len(t, result.GIFs, 2, "Slink results are still returned")

	// The budget frees up after an hour
	now = now.Add(61 * time.Minute)
	result = service.Search(context.Background(), "three", true)
	require.Equal(t, 3, requests["/v1/gifs/search"])
	require.Empty(t, result.Limited)
}

func TestKlipySearch(t *testing.T) {
	service, requests, _ := newTestService(t, Options{GiphyKey: "giphy-key", KlipyKey: "klipy-key", KlipyPerHour: 1})
	require.Equal(t, []string{ProviderGiphy, ProviderKlipy}, service.APIProviders())
	require.Equal(t, []string{service.slink[0].host, "*.giphy.com", "static.klipy.com"}, service.Hosts())

	service.Search(context.Background(), "cat", false)
	require.Zero(t, requests["/v2/search"], "typing only searches Slink")

	result := service.Search(context.Background(), "cat", true)
	require.Equal(t, 1, requests["/v2/search"])
	var klipy []GIF
	for _, gif := range result.GIFs {
		if gif.Provider == ProviderKlipy {
			klipy = append(klipy, gif)
		}
	}
	require.Equal(t, []GIF{
		{URL: "https://static.klipy.com/ii/abc/1c/6c/medium.gif", Preview: "https://static.klipy.com/ii/abc/1c/6c/tiny.gif", Width: 320, Height: 180, Title: "Cat Jam", Provider: ProviderKlipy, Source: "klipy.com"},
		{URL: "https://static.klipy.com/ii/def/full.gif", Preview: "https://static.klipy.com/ii/def/full.gif", Width: 200, Height: 100, Title: "Only full", Provider: ProviderKlipy, Source: "klipy.com"},
	}, klipy, "medium size preferred, other hosts skipped")

	// Each provider has its own budget
	result = service.Search(context.Background(), "dog", true)
	require.Equal(t, 1, requests["/v2/search"])
	require.Equal(t, 2, requests["/v1/gifs/search"])
	require.Equal(t, []string{ProviderKlipy}, result.Limited)
}
