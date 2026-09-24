package gifs

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveGiphyLink(t *testing.T) {
	resolver := newLinkResolver()
	for _, link := range []string{
		"https://giphy.com/gifs/cat-dance-abcDEF123",
		"https://giphy.com/gifs/abcDEF123",
		"https://www.giphy.com/stickers/happy-cat-abcDEF123/",
		"https://giphy.com/embed/abcDEF123",
	} {
		gif, err := resolver.resolve(context.Background(), link, nil)
		require.NoError(t, err, link)
		require.Equal(t, "https://media.giphy.com/media/abcDEF123/giphy.gif", gif.URL, link)
	}
}

func TestResolveUnsupportedLinks(t *testing.T) {
	resolver := newLinkResolver()
	for _, link := range []string{
		"https://example.com/gifs/cat",
		"http://klipy.com/gifs/cat-blink-9",
		"https://klipy.com/",
		"https://klipy.com/support/api-terms",
		"https://giphy.com/explore/cats",
		"not a link",
	} {
		_, err := resolver.resolve(context.Background(), link, nil)
		require.ErrorIs(t, err, ErrNotAPageLink, link)
	}
}

func TestParseKlipyPage(t *testing.T) {
	gif, err := parseKlipyPage(`<html><head>
		<meta property="og:title" content="Cat Blink - KLIPY">
		<meta property="og:image" content="https://static.klipy.com/ii/4e7b/1c/6c/fFBsIjDEwO7A.gif">
		<meta name="twitter:image" content="https://static.klipy.com/ii/4e7b/1c/6c/fFBsIjDEwO7A.jpg"/>
	</head></html>`)
	require.NoError(t, err)
	require.Equal(t, &GIF{URL: "https://static.klipy.com/ii/4e7b/1c/6c/fFBsIjDEwO7A.gif", Preview: "https://static.klipy.com/ii/4e7b/1c/6c/fFBsIjDEwO7A.gif", Title: "Cat Blink", Provider: ProviderKlipy, Source: "klipy.com"}, gif)

	// A still preview: the GIF in the same folder is used, not a related one
	gif, err = parseKlipyPage(`<meta content='https://static.klipy.com/ii/4e7b/1c/6c/still.jpg' property='og:image'>
		<script>{"related":"https:\/\/static.klipy.com\/ii\/9999\/aa\/bb\/other.gif","file":"https:\/\/static.klipy.com\/ii\/4e7b\/1c\/6c\/animated.gif"}</script>`)
	require.NoError(t, err)
	require.Equal(t, "https://static.klipy.com/ii/4e7b/1c/6c/animated.gif", gif.URL)

	gif, err = parseKlipyPage(`<meta property="og:video" content="https://static.klipy.com/ii/a/b/c/x.mp4"><meta property="og:image" content="https://static.klipy.com/ii/a/b/c/x.webp">`)
	require.NoError(t, err)
	require.Equal(t, "https://static.klipy.com/ii/a/b/c/x.webp", gif.URL, "animated image preferred over video")

	_, err = parseKlipyPage(`<meta property="og:image" content="https://cdn.elsewhere.com/x.gif">`)
	require.ErrorContains(t, err, "cdn.elsewhere.com")

	_, err = parseKlipyPage(`<html><head><title>Cat</title></head></html>`)
	require.Error(t, err)
}

// A resolver whose requests to klipy.com go to a test server
func newTestResolver(t *testing.T, handler http.HandlerFunc) *linkResolver {
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)

	transport := server.Client().Transport.(*http.Transport).Clone()
	transport.TLSClientConfig.ServerName = "example.com" // in the test certificate
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	resolver := newLinkResolver()
	resolver.client.Transport = transport
	return resolver
}

func TestResolveKlipyLink(t *testing.T) {
	var lock sync.Mutex
	pages := 0
	resolver := newTestResolver(t, func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		pages++
		lock.Unlock()
		switch r.URL.Path {
		case "/gifs/cat-blink-9":
			_, _ = w.Write([]byte(`<meta property="og:image" content="https://static.klipy.com/ii/4e7b/1c/6c/fFBsIjDEwO7A.gif">`))
		case "/gifs/moved":
			http.Redirect(w, r, "https://evil.example.com/", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	})

	gif, err := resolver.resolve(context.Background(), "https://klipy.com/gifs/cat-blink-9", nil)
	require.NoError(t, err)
	require.Equal(t, "https://static.klipy.com/ii/4e7b/1c/6c/fFBsIjDEwO7A.gif", gif.URL)

	_, err = resolver.resolve(context.Background(), "https://www.klipy.com/gifs/cat-blink-9/", nil)
	require.NoError(t, err)
	require.Equal(t, 1, pages, "cached")

	_, err = resolver.resolve(context.Background(), "https://klipy.com/gifs/moved", nil)
	require.ErrorIs(t, err, ErrGIFNotFound, "redirects off klipy.com are not followed")

	_, err = resolver.resolve(context.Background(), "https://klipy.com/gifs/missing-1", nil)
	require.ErrorIs(t, err, ErrGIFNotFound)
}

func TestResolveKlipyLinkThroughAPI(t *testing.T) {
	// The page has no usable preview, KLIPY's search finds it by its page link
	resolver := newTestResolver(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html></html>`))
	})

	api := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "cat blink", r.URL.Query().Get("q"))
		_, _ = w.Write([]byte(`{"results":[
			{"title":"Cat Blink","itemurl":"https://klipy.com/gifs/cat-blink-3","media_formats":{"gif":{"url":"https://static.klipy.com/ii/3/a/b/three.gif"}}},
			{"title":"Cat Blink","itemurl":"https://klipy.com/gifs/cat-blink-9","media_formats":{"gif":{"url":"https://static.klipy.com/ii/9/a/b/nine.gif"}}}
		]}`))
	}))
	t.Cleanup(api.Close)
	service := New(Options{KlipyKey: "klipy-key"})
	service.client = api.Client()
	service.apis[0].baseURL = api.URL

	gif, err := resolver.resolve(context.Background(), "https://klipy.com/gifs/cat-blink-9", service)
	require.NoError(t, err)
	require.Equal(t, "https://static.klipy.com/ii/9/a/b/nine.gif", gif.URL)
}
