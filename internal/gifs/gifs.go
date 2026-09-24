// Package gifs searches Giphy and Tenor for the chat GIF picker. Requests go
// through the server so API keys stay private, and results are cached so
// many viewers searching the same thing cost one request.
package gifs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/glimesh/broadcast-box/internal/environment"
)

const (
	ProviderGiphy = "giphy"
	ProviderTenor = "tenor"

	resultLimit      = 30
	cacheDuration    = 10 * time.Minute
	trendingDuration = 30 * time.Minute
	cacheSize        = 500
	requestTimeout   = 10 * time.Second
	maxQueryLength   = 50
	maxResponseBytes = 4 << 20
)

// Hosts the providers serve GIF files from, allowed in chat automatically
var providerHosts = map[string][]string{
	ProviderGiphy: {"*.giphy.com"},
	ProviderTenor: {"*.tenor.com"},
}

type GIF struct {
	URL      string `json:"url"`     // sent in chat
	Preview  string `json:"preview"` // smaller version for the picker
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	Title    string `json:"title,omitempty"`
	Provider string `json:"provider"`
}

type cacheEntry struct {
	gifs    []GIF
	expires time.Time
}

type Service struct {
	giphyKey string
	tenorKey string
	rating   string
	client   *http.Client
	baseURLs map[string]string

	lock  sync.Mutex
	cache map[string]cacheEntry
}

// DefaultService is nil when neither GIPHY_API_KEY nor TENOR_API_KEY is set
var DefaultService *Service

func Setup() {
	giphyKey, tenorKey := os.Getenv(environment.GiphyAPIKey), os.Getenv(environment.TenorAPIKey)
	if giphyKey == "" && tenorKey == "" {
		return
	}

	DefaultService = New(giphyKey, tenorKey, os.Getenv(environment.GIFContentRating))
	slog.Info("GIFs: search enabled", "providers", DefaultService.Providers(), "rating", DefaultService.rating)
}

// rating is Giphy's content rating (g, pg, pg-13, r), default pg-13
func New(giphyKey, tenorKey, rating string) *Service {
	rating = strings.ToLower(strings.TrimSpace(rating))
	switch rating {
	case "g", "pg", "pg-13", "r":
	default:
		rating = "pg-13"
	}

	return &Service{
		giphyKey: giphyKey,
		tenorKey: tenorKey,
		rating:   rating,
		client:   &http.Client{Timeout: requestTimeout},
		baseURLs: map[string]string{
			ProviderGiphy: "https://api.giphy.com",
			ProviderTenor: "https://tenor.googleapis.com",
		},
		cache: map[string]cacheEntry{},
	}
}

func (s *Service) Providers() (providers []string) {
	if s.giphyKey != "" {
		providers = append(providers, ProviderGiphy)
	}
	if s.tenorKey != "" {
		providers = append(providers, ProviderTenor)
	}
	return providers
}

// Hosts of the configured providers, to allow their GIFs in chat
func (s *Service) Hosts() (hosts []string) {
	for _, provider := range s.Providers() {
		hosts = append(hosts, providerHosts[provider]...)
	}
	return hosts
}

// Search returns GIFs for the query, or trending GIFs for an empty query
func (s *Service) Search(ctx context.Context, query string) []GIF {
	query = strings.TrimSpace(query)
	if len(query) > maxQueryLength {
		query = query[:maxQueryLength]
	}
	key := strings.ToLower(query)

	s.lock.Lock()
	entry, ok := s.cache[key]
	s.lock.Unlock()
	if ok && time.Now().Before(entry.expires) {
		return entry.gifs
	}

	fetchContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), requestTimeout)
	defer cancel()

	var (
		wait    sync.WaitGroup
		lock    sync.Mutex
		results = map[string][]GIF{}
	)
	for _, provider := range s.Providers() {
		wait.Go(func() {
			found, err := s.searchProvider(fetchContext, provider, query)
			if err != nil {
				slog.Error("GIFs: search failed", "provider", provider, "query", query, "err", err)
				return
			}
			lock.Lock()
			results[provider] = found
			lock.Unlock()
		})
	}
	wait.Wait()

	// Alternate providers so both show up at the top
	var merged []GIF
	for i := 0; i < resultLimit; i++ {
		for _, provider := range s.Providers() {
			if i < len(results[provider]) {
				merged = append(merged, results[provider][i])
			}
		}
	}

	duration := cacheDuration
	if query == "" {
		duration = trendingDuration
	}
	s.lock.Lock()
	if len(s.cache) >= cacheSize {
		for cached, entry := range s.cache {
			if time.Now().After(entry.expires) || len(s.cache) >= cacheSize {
				delete(s.cache, cached)
			}
		}
	}
	s.cache[key] = cacheEntry{gifs: merged, expires: time.Now().Add(duration)}
	s.lock.Unlock()

	return merged
}

func (s *Service) getJSON(ctx context.Context, requestURL string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return err
	}
	response, err := s.client.Do(request)
	if err != nil {
		// The error contains the URL with the API key
		return fmt.Errorf("request failed: %w", unwrapURLError(err))
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("returned %d", response.StatusCode)
	}
	return json.NewDecoder(http.MaxBytesReader(nil, response.Body, maxResponseBytes)).Decode(target)
}

func unwrapURLError(err error) error {
	if urlError, ok := err.(*url.Error); ok {
		return urlError.Err
	}
	return err
}

func (s *Service) searchProvider(ctx context.Context, provider, query string) ([]GIF, error) {
	switch provider {
	case ProviderGiphy:
		return s.searchGiphy(ctx, query)
	case ProviderTenor:
		return s.searchTenor(ctx, query)
	}
	return nil, nil
}

type giphyImage struct {
	URL    string `json:"url"`
	Width  string `json:"width"`
	Height string `json:"height"`
}

func (s *Service) searchGiphy(ctx context.Context, query string) ([]GIF, error) {
	params := url.Values{
		"api_key": {s.giphyKey},
		"limit":   {fmt.Sprint(resultLimit)},
		"rating":  {s.rating},
	}
	endpoint := "/v1/gifs/trending"
	if query != "" {
		endpoint = "/v1/gifs/search"
		params.Set("q", query)
	}

	var response struct {
		Data []struct {
			Title  string `json:"title"`
			Images struct {
				Original    giphyImage `json:"original"`
				FixedHeight giphyImage `json:"fixed_height"`
				FixedSmall  giphyImage `json:"fixed_height_small"`
			} `json:"images"`
		} `json:"data"`
	}
	if err := s.getJSON(ctx, s.baseURLs[ProviderGiphy]+endpoint+"?"+params.Encode(), &response); err != nil {
		return nil, err
	}

	var gifs []GIF
	for _, item := range response.Data {
		full := item.Images.FixedHeight
		if full.URL == "" {
			full = item.Images.Original
		}
		preview := item.Images.FixedSmall.URL
		if preview == "" {
			preview = full.URL
		}
		gifs = appendGIF(gifs, GIF{
			URL:      stripQuery(full.URL),
			Preview:  stripQuery(preview),
			Width:    atoi(full.Width),
			Height:   atoi(full.Height),
			Title:    item.Title,
			Provider: ProviderGiphy,
		})
	}
	return gifs, nil
}

func (s *Service) searchTenor(ctx context.Context, query string) ([]GIF, error) {
	contentFilter := map[string]string{"g": "high", "pg": "medium", "pg-13": "low", "r": "off"}[s.rating]
	params := url.Values{
		"key":           {s.tenorKey},
		"client_key":    {"broadcast-box"},
		"limit":         {fmt.Sprint(resultLimit)},
		"media_filter":  {"gif,tinygif"},
		"contentfilter": {contentFilter},
	}
	endpoint := "/v2/featured"
	if query != "" {
		endpoint = "/v2/search"
		params.Set("q", query)
	}

	type tenorMedia struct {
		URL  string `json:"url"`
		Dims []int  `json:"dims"`
	}
	var response struct {
		Results []struct {
			Title              string                `json:"title"`
			ContentDescription string                `json:"content_description"`
			MediaFormats       map[string]tenorMedia `json:"media_formats"`
		} `json:"results"`
	}
	if err := s.getJSON(ctx, s.baseURLs[ProviderTenor]+endpoint+"?"+params.Encode(), &response); err != nil {
		return nil, err
	}

	var gifs []GIF
	for _, item := range response.Results {
		full := item.MediaFormats["gif"]
		preview := item.MediaFormats["tinygif"]
		if preview.URL == "" {
			preview = full
		}
		title := item.Title
		if title == "" {
			title = item.ContentDescription
		}
		gif := GIF{URL: full.URL, Preview: preview.URL, Title: title, Provider: ProviderTenor}
		if len(full.Dims) == 2 {
			gif.Width, gif.Height = full.Dims[0], full.Dims[1]
		}
		gifs = appendGIF(gifs, gif)
	}
	return gifs, nil
}

// Keeps GIFs that are served from the provider's own hosts
func appendGIF(gifs []GIF, gif GIF) []GIF {
	hosts := providerHosts[gif.Provider]
	if !IsAllowedURL(gif.URL, hosts) || !IsAllowedURL(gif.Preview, hosts) {
		return gifs
	}
	return append(gifs, gif)
}

// Giphy adds tracking parameters, the file is the same without them
func stripQuery(value string) string {
	if parsed, err := url.Parse(value); err == nil {
		parsed.RawQuery = ""
		return parsed.String()
	}
	return value
}

func atoi(value string) int {
	number := 0
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0
		}
		number = number*10 + int(character-'0')
	}
	return number
}

// MatchesHost reports whether host matches a pattern: an exact host name,
// "*.example.com" for example.com and all its subdomains, or "*" for any host
func MatchesHost(pattern, host string) bool {
	pattern, host = strings.ToLower(pattern), strings.ToLower(host)
	switch {
	case pattern == "*":
		return true
	case strings.HasPrefix(pattern, "*."):
		domain := pattern[2:]
		return host == domain || strings.HasSuffix(host, "."+domain)
	default:
		return host == pattern
	}
}

// IsAllowedURL reports whether value is an https URL on one of the hosts
func IsAllowedURL(value string, hosts []string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return false
	}
	for _, pattern := range hosts {
		if MatchesHost(pattern, parsed.Hostname()) {
			return true
		}
	}
	return false
}
