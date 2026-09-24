package gifs

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Viewers often paste the link of a GIF's web page (https://klipy.com/gifs/cat-blink-9,
// https://giphy.com/gifs/cat-abc123) instead of the GIF file itself. Chat can
// only show the file, so these links are resolved to the GIF behind them.

var (
	ErrNotAPageLink = errors.New("not a link to a supported GIF page")
	ErrGIFNotFound  = errors.New("no GIF found on the page")
)

const (
	linkCacheDuration = 24 * time.Hour
	linkRetryDuration = 5 * time.Minute
	linkCacheSize     = 500
	maxPageBytes      = 2 << 20
)

var (
	// giphy.com/gifs/<slug>-<id>, giphy.com/gifs/<id>, giphy.com/stickers/..., giphy.com/embed/<id>
	giphyPagePath = regexp.MustCompile(`^/(?:gifs|stickers|embed)/(?:[^/]*-)?([A-Za-z0-9]+)/?$`)
	klipyPagePath = regexp.MustCompile(`^/(?:[a-z]{2}(?:-[a-z]{2})?/)?(?:gifs|stickers|clips|memes)/([^/]+)/?$`)

	metaTag       = regexp.MustCompile(`(?is)<meta\s[^>]*>`)
	metaAttribute = regexp.MustCompile(`(?is)(property|name|content)\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	klipyFileURL  = regexp.MustCompile(`https://static\.klipy\.com/[^"'\s<>\\)]+`)

	// Page preview tags, the ones Discord and others use to embed a link
	previewTags = []string{"og:image:secure_url", "og:image", "og:image:url", "twitter:image", "twitter:image:src", "og:video:secure_url", "og:video", "og:video:url"}

	klipyFileHosts = []string{"static.klipy.com"}
)

type resolvedEntry struct {
	gif     *GIF
	expires time.Time
}

type linkResolver struct {
	client *http.Client
	now    func() time.Time

	lock  sync.Mutex
	cache map[string]resolvedEntry
}

func newLinkResolver() *linkResolver {
	return &linkResolver{
		client: &http.Client{
			Timeout: requestTimeout,
			CheckRedirect: func(request *http.Request, via []*http.Request) error {
				if len(via) >= 5 || request.URL.Scheme != "https" || !isKlipyPageHost(request.URL.Hostname()) {
					return fmt.Errorf("redirect to %s not followed", request.URL.Host)
				}
				return nil
			},
		},
		now:   time.Now,
		cache: map[string]resolvedEntry{},
	}
}

var defaultLinkResolver = newLinkResolver()

func isKlipyPageHost(host string) bool {
	host = strings.ToLower(host)
	return host == "klipy.com" || host == "www.klipy.com"
}

// ResolveLink returns the GIF behind the link of a KLIPY or Giphy page
func ResolveLink(ctx context.Context, link string) (*GIF, error) {
	return defaultLinkResolver.resolve(ctx, link, DefaultService)
}

func (r *linkResolver) resolve(ctx context.Context, link string, service *Service) (*GIF, error) {
	parsed, err := url.Parse(strings.TrimSpace(link))
	if err != nil || parsed.Scheme != "https" {
		return nil, ErrNotAPageLink
	}
	host := strings.ToLower(parsed.Hostname())

	// Giphy page links carry the GIF's ID, no need to fetch anything
	if host == "giphy.com" || host == "www.giphy.com" {
		match := giphyPagePath.FindStringSubmatch(parsed.Path)
		if match == nil {
			return nil, ErrNotAPageLink
		}
		fileURL := "https://media.giphy.com/media/" + match[1] + "/giphy.gif"
		return &GIF{URL: fileURL, Preview: "https://media.giphy.com/media/" + match[1] + "/200w.gif", Provider: ProviderGiphy, Source: "giphy.com"}, nil
	}

	if !isKlipyPageHost(host) || klipyPagePath.FindStringSubmatch(parsed.Path) == nil {
		return nil, ErrNotAPageLink
	}
	pageURL := "https://klipy.com" + strings.TrimSuffix(parsed.Path, "/")

	r.lock.Lock()
	entry, ok := r.cache[pageURL]
	r.lock.Unlock()
	if ok && r.now().Before(entry.expires) {
		if entry.gif == nil {
			return nil, ErrGIFNotFound
		}
		return entry.gif, nil
	}

	fetchContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), requestTimeout)
	defer cancel()

	gif, pageErr := r.fromKlipyPage(fetchContext, pageURL)
	if gif == nil && service != nil {
		var apiErr error
		if gif, apiErr = service.findKlipyPage(fetchContext, pageURL); apiErr != nil {
			pageErr = errors.Join(pageErr, apiErr)
		}
	}
	if gif == nil {
		slog.Warn("GIFs: no GIF found for a KLIPY link", "link", pageURL, "err", pageErr)
	}

	r.lock.Lock()
	if len(r.cache) >= linkCacheSize {
		clear(r.cache)
	}
	// Failures may be temporary, try again sooner
	duration := linkCacheDuration
	if gif == nil {
		duration = linkRetryDuration
	}
	r.cache[pageURL] = resolvedEntry{gif: gif, expires: r.now().Add(duration)}
	r.lock.Unlock()

	if gif == nil {
		return nil, ErrGIFNotFound
	}
	return gif, nil
}

// Reads the GIF from the page's preview tags (og:image and the like)
func (r *linkResolver) fromKlipyPage(ctx context.Context, pageURL string) (*GIF, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "text/html")
	request.Header.Set("User-Agent", "Mozilla/5.0 (compatible; BroadcastBox link preview; +https://github.com/Glimesh/broadcast-box)")

	response, err := r.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("page request failed: %w", unwrapURLError(err))
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("page returned %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxPageBytes))
	if err != nil {
		return nil, fmt.Errorf("reading page failed: %w", err)
	}
	return parseKlipyPage(string(body))
}

func parseKlipyPage(page string) (*GIF, error) {
	tags := map[string]string{}
	for _, tag := range metaTag.FindAllString(page, -1) {
		var key, content string
		for _, attribute := range metaAttribute.FindAllStringSubmatch(tag, -1) {
			value := html.UnescapeString(attribute[2] + attribute[3])
			if strings.EqualFold(attribute[1], "content") {
				content = value
			} else {
				key = strings.ToLower(value)
			}
		}
		if _, seen := tags[key]; key != "" && content != "" && !seen {
			tags[key] = content
		}
	}

	var candidates, skipped []string
	for _, name := range previewTags {
		if value := tags[name]; value != "" {
			if IsAllowedURL(value, klipyFileHosts) {
				candidates = append(candidates, value)
			} else {
				skipped = append(skipped, value)
			}
		}
	}
	if len(candidates) == 0 {
		if len(skipped) > 0 {
			return nil, fmt.Errorf("page preview is not on %v: %v", klipyFileHosts, skipped)
		}
		return nil, errors.New("page has no preview image")
	}

	fileURL := pickFile(candidates)
	if extension := fileExtension(fileURL); extension != ".gif" && extension != ".webp" {
		// A still preview: the animated GIF is usually stored next to it
		directory := path.Dir(mustParse(fileURL).Path)
		for _, found := range klipyFileURL.FindAllString(strings.ReplaceAll(page, `\/`, "/"), -1) {
			if fileExtension(found) == ".gif" && path.Dir(mustParse(found).Path) == directory {
				fileURL = found
				break
			}
		}
	}
	if extension := fileExtension(fileURL); extension == ".mp4" || extension == ".webm" {
		return nil, fmt.Errorf("page only has a video: %s", fileURL)
	}

	title := strings.TrimSpace(tags["og:title"])
	title = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(title, " - KLIPY"), " | KLIPY"))
	return &GIF{URL: fileURL, Preview: fileURL, Title: title, Provider: ProviderKlipy, Source: "klipy.com"}, nil
}

// Animated formats first
func pickFile(candidates []string) string {
	for _, extension := range []string{".gif", ".webp"} {
		for _, candidate := range candidates {
			if fileExtension(candidate) == extension {
				return candidate
			}
		}
	}
	for _, candidate := range candidates {
		if extension := fileExtension(candidate); extension != ".mp4" && extension != ".webm" {
			return candidate
		}
	}
	return candidates[0]
}

func fileExtension(value string) string {
	return strings.ToLower(path.Ext(mustParse(value).Path))
}

func mustParse(value string) *url.URL {
	parsed, err := url.Parse(value)
	if err != nil {
		return &url.URL{}
	}
	return parsed
}

// Falls back to KLIPY's search API: searches the words of the page's slug
// and picks the result whose page is the link. Uses the hourly budget.
func (s *Service) findKlipyPage(ctx context.Context, pageURL string) (*GIF, error) {
	var provider *apiProvider
	for _, candidate := range s.apis {
		if candidate.name == ProviderKlipy {
			provider = candidate
		}
	}
	if provider == nil {
		return nil, nil
	}

	slug := path.Base(pageURL)
	words := strings.Fields(strings.ReplaceAll(slug, "-", " "))
	if len(words) > 1 && strings.Trim(words[len(words)-1], "0123456789") == "" {
		words = words[:len(words)-1] // cat-blink-9: the number tells apart GIFs with the same name
	}
	query := strings.Join(words, " ")

	gifs := s.cached(ProviderKlipy+":"+strings.ToLower(query), apiCacheDuration, func() ([]GIF, error) {
		if !s.takeRequest(provider) {
			return nil, errBudgetUsedUp
		}
		return provider.search(ctx, provider, query)
	})
	for _, gif := range gifs {
		if page := mustParse(gif.pageURL); isKlipyPageHost(page.Hostname()) && strings.TrimSuffix(page.Path, "/") == strings.TrimPrefix(pageURL, "https://klipy.com") {
			return &gif, nil
		}
	}
	return nil, fmt.Errorf("not among KLIPY's search results for %q", query)
}
