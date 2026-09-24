package handlers

import (
	"errors"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/glimesh/broadcast-box/internal/emotes"
	"github.com/glimesh/broadcast-box/internal/environment"
	"github.com/glimesh/broadcast-box/internal/gifs"
	"github.com/glimesh/broadcast-box/internal/server/helpers"
	"golang.org/x/time/rate"
)

// GET /api/chat/emotes?key=<streamKey> lists the stream's Twitch, 7TV,
// BetterTTV and FrankerFaceZ emotes, and the hosts GIFs may be shown from
func emotesHandler(responseWriter http.ResponseWriter, request *http.Request) {
	list := []emotes.Emote{}
	if emotes.DefaultService != nil {
		if found := emotes.DefaultService.ForStream(request.Context(), request.URL.Query().Get("key")); found != nil {
			list = found
		}
	}

	responseWriter.Header().Set("Cache-Control", "public, max-age=300")
	gifSources := map[string]any{"apis": []string{}, "slink": []string{}}
	if gifs.DefaultService != nil {
		gifSources["apis"] = gifs.DefaultService.APIProviders()
		if hosts := gifs.DefaultService.SlinkHosts(); hosts != nil {
			gifSources["slink"] = hosts
		}
	}

	writeJSON(responseWriter, map[string]any{
		"emotes":     list,
		"gifHosts":   gifHosts(),
		"gifSources": gifSources,
	})
}

// GET /api/chat/gifs/search?q=<query>[&apis] searches the Slink instances,
// and Giphy and KLIPY only with the apis parameter (the viewer pressed
// enter). An empty query lists the newest Slink images.
func gifSearchHandler(responseWriter http.ResponseWriter, request *http.Request) {
	if gifs.DefaultService == nil {
		writeJSON(responseWriter, gifs.SearchResult{GIFs: []gifs.GIF{}})
		return
	}
	if !gifSearchLimiters.allow(clientAddress(request)) {
		helpers.LogHTTPError(responseWriter, "Too many searches", http.StatusTooManyRequests)
		return
	}

	_, includeAPIs := request.URL.Query()["apis"]
	result := gifs.DefaultService.Search(request.Context(), request.URL.Query().Get("q"), includeAPIs)
	if result.GIFs == nil {
		result.GIFs = []gifs.GIF{}
	}
	responseWriter.Header().Set("Cache-Control", "public, max-age=60")
	writeJSON(responseWriter, result)
}

// GET /api/chat/gifs/resolve?url=<link> returns the GIF behind the link of a
// KLIPY or Giphy web page, chat can only show the GIF file itself
func gifResolveHandler(responseWriter http.ResponseWriter, request *http.Request) {
	if !gifSearchLimiters.allow(clientAddress(request)) {
		helpers.LogHTTPError(responseWriter, "Too many searches", http.StatusTooManyRequests)
		return
	}

	gif, err := gifs.ResolveLink(request.Context(), request.URL.Query().Get("url"))
	if err != nil {
		reason := "unsupported"
		if errors.Is(err, gifs.ErrGIFNotFound) {
			reason = "not_found"
		}
		responseWriter.Header().Set("Content-Type", "application/json")
		responseWriter.WriteHeader(http.StatusNotFound)
		writeJSON(responseWriter, map[string]string{"error": reason})
		return
	}
	responseWriter.Header().Set("Cache-Control", "public, max-age=86400")
	writeJSON(responseWriter, gif)
}

// GET /api/chat/emotes/search?q=<query> searches 7TV, BetterTTV and FrankerFaceZ
func emoteSearchHandler(responseWriter http.ResponseWriter, request *http.Request) {
	if emotes.DefaultService == nil {
		writeJSON(responseWriter, map[string]any{"emotes": []emotes.Emote{}})
		return
	}
	if !searchLimiters.allow(clientAddress(request)) {
		helpers.LogHTTPError(responseWriter, "Too many searches", http.StatusTooManyRequests)
		return
	}

	list := emotes.DefaultService.Search(request.Context(), request.URL.Query().Get("q"))
	if list == nil {
		list = []emotes.Emote{}
	}
	responseWriter.Header().Set("Cache-Control", "public, max-age=600")
	writeJSON(responseWriter, map[string]any{"emotes": list})
}

// Hosts whose GIF links are shown in chat: CHAT_GIF_HOSTS ("gifs.example.com",
// "*.giphy.com" for a domain and its subdomains, "*" for any https host) and
// the hosts of the configured GIF search providers
var gifHosts = sync.OnceValue(func() []string {
	hosts := []string{}
	for host := range strings.SplitSeq(os.Getenv(environment.ChatGIFHosts), ",") {
		if host = strings.ToLower(strings.TrimSpace(host)); host != "" {
			hosts = append(hosts, host)
		}
	}
	if gifs.DefaultService != nil {
		for _, host := range gifs.DefaultService.Hosts() {
			if !slices.Contains(hosts, host) {
				hosts = append(hosts, host)
			}
		}
	}
	return hosts
})

type clientLimiters struct {
	lock     sync.Mutex
	limit    rate.Limit
	burst    int
	limiters map[string]*clientLimiterEntry
}

type clientLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// Typing in the search box sends a request per pause, allow a few per second
var (
	searchLimiters    = &clientLimiters{limit: 2, burst: 8, limiters: map[string]*clientLimiterEntry{}}
	gifSearchLimiters = &clientLimiters{limit: 2, burst: 8, limiters: map[string]*clientLimiterEntry{}}
)

func (c *clientLimiters) allow(client string) bool {
	c.lock.Lock()
	defer c.lock.Unlock()

	now := time.Now()
	if len(c.limiters) > 10000 {
		for key, entry := range c.limiters {
			if now.Sub(entry.lastSeen) > 10*time.Minute {
				delete(c.limiters, key)
			}
		}
	}

	entry, ok := c.limiters[client]
	if !ok {
		entry = &clientLimiterEntry{limiter: rate.NewLimiter(c.limit, c.burst)}
		c.limiters[client] = entry
	}
	entry.lastSeen = now
	return entry.limiter.Allow()
}
