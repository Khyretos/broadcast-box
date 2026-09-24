// Package gifs searches GIFs for the chat picker: self hosted Slink
// instances (https://github.com/andrii-kryvoviaz/slink) and Giphy. Requests go
// through the server so API keys stay private, and results are cached.
package gifs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/glimesh/broadcast-box/internal/environment"
)

const (
	ProviderGiphy = "giphy"
	ProviderSlink = "slink"

	resultLimit      = 30
	requestTimeout   = 10 * time.Second
	maxQueryLength   = 50
	maxResponseBytes = 4 << 20
	cacheSize        = 500

	// Giphy's free API keys allow 100 requests per hour
	giphyCacheDuration  = time.Hour
	defaultGiphyPerHour = 90

	// Own servers without a quota, short so new uploads show up quickly
	slinkCacheDuration = time.Minute
)

type GIF struct {
	URL      string `json:"url"`     // sent in chat
	Preview  string `json:"preview"` // shown in the picker
	Width    int    `json:"width,omitempty"`
	Height   int    `json:"height,omitempty"`
	Title    string `json:"title,omitempty"`
	Provider string `json:"provider"`
	Source   string `json:"source"` // host the GIF comes from
}

type SearchResult struct {
	GIFs []GIF `json:"gifs"`
	// Giphy was requested but skipped because the hourly budget is used up
	GiphyLimited bool `json:"giphyLimited,omitempty"`
}

type slinkInstance struct {
	baseURL string // e.g. https://gifs.example.com
	host    string
}

type cacheEntry struct {
	gifs    []GIF
	expires time.Time
}

type Service struct {
	giphyKey      string
	giphyPerHour  int
	rating        string
	slink         []slinkInstance
	client        *http.Client
	giphyBaseURL  string
	now           func() time.Time
	giphyRequests []time.Time // within the last hour

	lock  sync.Mutex
	cache map[string]cacheEntry
}

// DefaultService is nil when neither GIPHY_API_KEY nor SLINK_INSTANCES is set
var DefaultService *Service

func Setup() {
	if os.Getenv(environment.TenorAPIKey) != "" {
		slog.Warn("GIFs: Tenor's API is no longer available, TENOR_API_KEY is ignored")
	}

	giphyPerHour, _ := strconv.Atoi(os.Getenv(environment.GiphyHourlyLimit))
	service := New(os.Getenv(environment.GiphyAPIKey), giphyPerHour, os.Getenv(environment.GIFContentRating), ParseSlinkInstances(os.Getenv(environment.SlinkInstances)))
	if !service.GiphyEnabled() && len(service.slink) == 0 {
		return
	}

	DefaultService = service
	slog.Info("GIFs: search enabled", "giphy", service.GiphyEnabled(), "giphyPerHour", service.giphyPerHour, "slink", service.SlinkHosts())
}

// ParseSlinkInstances reads "https://gifs.example.com,other.example.com".
// Slink API keys only allow uploading, searching public images needs none,
// so a key appended as "host:sk_..." is ignored.
func ParseSlinkInstances(value string) (instances []slinkInstance) {
	for entry := range strings.SplitSeq(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if index := strings.Index(entry, ":sk_"); index >= 0 {
			slog.Warn("GIFs: Slink API keys are only used for uploading and are not needed to search, ignoring the key", "instance", entry[:index])
			entry = entry[:index]
		}
		if !strings.Contains(entry, "://") {
			entry = "https://" + entry
		}

		// Chat only shows images served over https
		parsed, err := url.Parse(entry)
		if err != nil || parsed.Host == "" || parsed.Scheme != "https" {
			slog.Error("GIFs: invalid Slink instance ignored, use its public https address", "instance", entry)
			continue
		}
		instances = append(instances, slinkInstance{
			baseURL: parsed.Scheme + "://" + parsed.Host + strings.TrimSuffix(parsed.Path, "/"),
			host:    strings.ToLower(parsed.Hostname()),
		})
	}
	return instances
}

// rating is Giphy's content rating (g, pg, pg-13, r), default pg-13
func New(giphyKey string, giphyPerHour int, rating string, slink []slinkInstance) *Service {
	rating = strings.ToLower(strings.TrimSpace(rating))
	switch rating {
	case "g", "pg", "pg-13", "r":
	default:
		rating = "pg-13"
	}
	if giphyPerHour <= 0 {
		giphyPerHour = defaultGiphyPerHour
	}

	return &Service{
		giphyKey:     giphyKey,
		giphyPerHour: giphyPerHour,
		rating:       rating,
		slink:        slink,
		client:       &http.Client{Timeout: requestTimeout},
		giphyBaseURL: "https://api.giphy.com",
		now:          time.Now,
		cache:        map[string]cacheEntry{},
	}
}

func (s *Service) GiphyEnabled() bool { return s.giphyKey != "" }

func (s *Service) SlinkHosts() (hosts []string) {
	for _, instance := range s.slink {
		hosts = append(hosts, instance.host)
	}
	return hosts
}

// Hosts GIFs are served from, allowed in chat automatically
func (s *Service) Hosts() []string {
	hosts := s.SlinkHosts()
	if s.GiphyEnabled() {
		hosts = append(hosts, "*.giphy.com")
	}
	return hosts
}

// Search searches the Slink instances, and Giphy when includeGiphy is set.
// An empty query lists the newest images of the Slink instances; Giphy is
// never searched without a query, to save its hourly request budget.
func (s *Service) Search(ctx context.Context, query string, includeGiphy bool) SearchResult {
	query = strings.TrimSpace(query)
	if len(query) > maxQueryLength {
		query = query[:maxQueryLength]
	}

	fetchContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), requestTimeout)
	defer cancel()

	var (
		wait    sync.WaitGroup
		lock    sync.Mutex
		result  SearchResult
		lists   = make([][]GIF, len(s.slink)+1)
		limited bool
	)
	for i, instance := range s.slink {
		wait.Go(func() {
			lists[i] = s.cached("slink:"+instance.host+":"+strings.ToLower(query), slinkCacheDuration, func() ([]GIF, error) {
				return s.searchSlink(fetchContext, instance, query)
			})
		})
	}
	if includeGiphy && query != "" && s.GiphyEnabled() {
		wait.Go(func() {
			found := s.cached("giphy:"+strings.ToLower(query), giphyCacheDuration, func() ([]GIF, error) {
				if !s.takeGiphyRequest() {
					lock.Lock()
					limited = true
					lock.Unlock()
					return nil, errGiphyLimited
				}
				return s.searchGiphy(fetchContext, query)
			})
			lists[len(s.slink)] = found
		})
	}
	wait.Wait()

	// Interleave sources so each shows up at the top
	for i := 0; i < resultLimit; i++ {
		for _, list := range lists {
			if i < len(list) {
				result.GIFs = append(result.GIFs, list[i])
			}
		}
	}
	result.GiphyLimited = limited
	return result
}

var errGiphyLimited = fmt.Errorf("giphy hourly request budget used up")

// Keeps Giphy under its hourly request limit, shared by all viewers
func (s *Service) takeGiphyRequest() bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	hourAgo := s.now().Add(-time.Hour)
	recent := s.giphyRequests[:0]
	for _, at := range s.giphyRequests {
		if at.After(hourAgo) {
			recent = append(recent, at)
		}
	}
	s.giphyRequests = recent

	if len(s.giphyRequests) >= s.giphyPerHour {
		return false
	}
	s.giphyRequests = append(s.giphyRequests, s.now())
	return true
}

func (s *Service) cached(key string, duration time.Duration, fetch func() ([]GIF, error)) []GIF {
	s.lock.Lock()
	entry, ok := s.cache[key]
	s.lock.Unlock()
	if ok && s.now().Before(entry.expires) {
		return entry.gifs
	}

	gifs, err := fetch()
	if err != nil {
		if err != errGiphyLimited {
			slog.Error("GIFs: search failed", "search", key, "err", err)
		} else {
			slog.Warn("GIFs: Giphy hourly budget used up, showing cached and Slink results only", "perHour", s.giphyPerHour)
		}
		return entry.gifs // possibly stale, better than nothing
	}

	s.lock.Lock()
	if len(s.cache) >= cacheSize {
		for cached, entry := range s.cache {
			if s.now().After(entry.expires) || len(s.cache) >= cacheSize {
				delete(s.cache, cached)
			}
		}
	}
	s.cache[key] = cacheEntry{gifs: gifs, expires: s.now().Add(duration)}
	s.lock.Unlock()
	return gifs
}

func (s *Service) getJSON(ctx context.Context, requestURL string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")

	response, err := s.client.Do(request)
	if err != nil {
		// The error contains the URL, which may contain an API key
		return fmt.Errorf("request failed: %w", unwrapURLError(err))
	}
	defer response.Body.Close()

	switch {
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		return fmt.Errorf("returned %d, access denied", response.StatusCode)
	case response.StatusCode != http.StatusOK:
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

// Slink's public image listing (GET /api/images), the same one its explore
// page uses. Needs guest viewing enabled on the instance
// (USER_ALLOW_UNAUTHENTICATED_ACCESS), and only returns public images.
// searchTerm matches the image description and the uploader's name.
func (s *Service) searchSlink(ctx context.Context, instance slinkInstance, query string) ([]GIF, error) {
	params := url.Values{"limit": {strconv.Itoa(resultLimit)}}
	if query != "" {
		params.Set("searchTerm", query)
	}

	var response struct {
		Data []struct {
			URL        string `json:"url"`
			Attributes struct {
				FileName    string `json:"fileName"`
				Description string `json:"description"`
				IsPublic    bool   `json:"isPublic"`
			} `json:"attributes"`
			Metadata *struct {
				MimeType string `json:"mimeType"`
				Width    int    `json:"width"`
				Height   int    `json:"height"`
			} `json:"metadata"`
		} `json:"data"`
	}
	if err := s.getJSON(ctx, instance.baseURL+"/api/images?"+params.Encode(), &response); err != nil {
		if strings.Contains(err.Error(), "access denied") {
			return nil, fmt.Errorf("%s: %w (enable guest access in Slink with USER_ALLOW_UNAUTHENTICATED_ACCESS=true)", instance.host, err)
		}
		return nil, fmt.Errorf("%s: %w", instance.host, err)
	}

	var gifs []GIF
	for _, item := range response.Data {
		if !item.Attributes.IsPublic || item.Metadata == nil || !strings.HasPrefix(item.Metadata.MimeType, "image/") {
			continue
		}

		// Slink returns the public image path, e.g. /api/image/public/<id>.gif
		imageURL := item.URL
		if strings.HasPrefix(imageURL, "/") {
			imageURL = instance.baseURL + imageURL
		}
		if !IsAllowedURL(imageURL, []string{instance.host}) {
			continue
		}

		title := item.Attributes.Description
		if title == "" {
			title = strings.TrimSuffix(item.Attributes.FileName, "."+strings.TrimPrefix(item.Metadata.MimeType, "image/"))
		}
		gifs = append(gifs, GIF{
			URL:      imageURL,
			Preview:  imageURL,
			Width:    item.Metadata.Width,
			Height:   item.Metadata.Height,
			Title:    title,
			Provider: ProviderSlink,
			Source:   instance.host,
		})
	}
	return gifs, nil
}

type giphyImage struct {
	URL    string `json:"url"`
	Width  string `json:"width"`
	Height string `json:"height"`
}

func (s *Service) searchGiphy(ctx context.Context, query string) ([]GIF, error) {
	params := url.Values{
		"api_key": {s.giphyKey},
		"limit":   {strconv.Itoa(resultLimit)},
		"rating":  {s.rating},
		"q":       {query},
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
	if err := s.getJSON(ctx, s.giphyBaseURL+"/v1/gifs/search?"+params.Encode(), &response); err != nil {
		return nil, err
	}

	hosts := []string{"*.giphy.com"}
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
		gif := GIF{
			URL:      stripQuery(full.URL),
			Preview:  stripQuery(preview),
			Width:    atoi(full.Width),
			Height:   atoi(full.Height),
			Title:    item.Title,
			Provider: ProviderGiphy,
			Source:   "giphy.com",
		}
		if IsAllowedURL(gif.URL, hosts) && IsAllowedURL(gif.Preview, hosts) {
			gifs = append(gifs, gif)
		}
	}
	return gifs, nil
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
	number, err := strconv.Atoi(value)
	if err != nil {
		return 0
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
