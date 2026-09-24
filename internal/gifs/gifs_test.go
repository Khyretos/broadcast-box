package gifs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

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

func TestSearch(t *testing.T) {
	requests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests[r.URL.Path]++
		switch r.URL.Path {
		case "/v1/gifs/search":
			require.Equal(t, "giphy-key", r.URL.Query().Get("api_key"))
			require.Equal(t, "cat", r.URL.Query().Get("q"))
			require.Equal(t, "pg-13", r.URL.Query().Get("rating"))
			_, _ = w.Write([]byte(`{"data":[
				{"title":"Cat Dance","images":{
					"fixed_height":{"url":"https://media3.giphy.com/media/abc/200.gif?cid=track&rid=200.gif","width":"356","height":"200"},
					"fixed_height_small":{"url":"https://media3.giphy.com/media/abc/100.gif?cid=track"},
					"original":{"url":"https://media3.giphy.com/media/abc/giphy.gif"}}},
				{"title":"Evil","images":{"fixed_height":{"url":"https://evil.example.com/x.gif"},"fixed_height_small":{"url":"https://evil.example.com/y.gif"}}}
			]}`))
		case "/v1/gifs/trending":
			_, _ = w.Write([]byte(`{"data":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := New("giphy-key", "")
	for provider := range service.baseURLs {
		service.baseURLs[provider] = server.URL
	}

	results := service.Search(context.Background(), "cat")
	require.Equal(t, []GIF{
		{URL: "https://media3.giphy.com/media/abc/200.gif", Preview: "https://media3.giphy.com/media/abc/100.gif", Width: 356, Height: 200, Title: "Cat Dance", Provider: ProviderGiphy},
	}, results)

	service.Search(context.Background(), "CAT")
	require.Equal(t, 1, requests["/v1/gifs/search"], "cached case insensitively")

	service.Search(context.Background(), "")
	require.Equal(t, 1, requests["/v1/gifs/trending"], "empty query shows trending")

	require.Equal(t, []string{"*.giphy.com"}, service.Hosts())
}
