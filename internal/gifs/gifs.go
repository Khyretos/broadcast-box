// Package gifs searches GIFs for the chat picker: self hosted Slink
// instances (https://github.com/andrii-kryvoviaz/slink), Giphy and KLIPY.
// Requests go through the server so API keys stay private, and results are
// cached.
package gifs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/glimesh/broadcast-box/internal/environment"
)

const (
	ProviderGiphy = "giphy"
	ProviderKlipy = "klipy"
	ProviderSlink = "slink"

	resultLimit      = 30
	requestTimeout   = 10 * time.Second
	maxQueryLength   = 50
	maxResponseBytes = 4 << 20
	cacheSize        = 500

	// Giphy's free API keys allow 100 requests per hour. Giphy and KLIPY are
	// only searched when a viewer presses enter, and results are kept long.
	apiCacheDuration    = time.Hour
	defaultGiphyPerHour = 90
	defaultKlipyPerHour = 100

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

	pageURL string // the GIF's web page, for resolving pasted page links
}

type SearchResult struct {
	GIFs []GIF `json:"gifs"`
	// API providers that were skipped because their hourly budget is used up
	Limited []string `json:"limited,omitempty"`
}

type slinkInstance struct {
	baseURL string // e.g. https://gifs.example.com
	host    string
}

type cacheEntry struct {
	gifs    []GIF
	expires time.Time
}

// A GIF service that needs an API key and has a request quota: only searched
// with a query the viewer pressed enter on, and within an hourly budget
type apiProvider struct {
	name     string
	key      string
	perHour  int
	baseURL  string
	hosts    []string // where its GIFs are served from
	search   func(ctx context.Context, provider *apiProvider, query string) ([]GIF, error)
	requests []time.Time // within the last hour
}

type Options struct {
	GiphyKey     string
	GiphyPerHour int
	KlipyKey     string
	KlipyPerHour int
	// Giphy's content rating (g, pg, pg-13, r), default pg-13
	Rating string
	Slink  []slinkInstance
}

type Service struct {
	rating string
	slink  []slinkInstance
	apis   []*apiProvider
	client *http.Client
	now    func() time.Time

	lock  sync.Mutex
	cache map[string]cacheEntry
}

// DefaultService is nil when no GIF search is configured
var DefaultService *Service

func Setup() {
	if os.Getenv(environment.TenorAPIKey) != "" {
		slog.Warn("GIFs: Tenor's API is no longer available, TENOR_API_KEY is ignored (KLIPY_API_KEY is a replacement)")
	}

	giphyPerHour, _ := strconv.Atoi(os.Getenv(environment.GiphyHourlyLimit))
	klipyPerHour, _ := strconv.Atoi(os.Getenv(environment.KlipyHourlyLimit))
	service := New(Options{
		GiphyKey:     strings.TrimSpace(os.Getenv(environment.GiphyAPIKey)),
		GiphyPerHour: giphyPerHour,
		KlipyKey:     strings.TrimSpace(os.Getenv(environment.KlipyAPIKey)),
		KlipyPerHour: klipyPerHour,
		Rating:       os.Getenv(environment.GIFContentRating),
		Slink:        ParseSlinkInstances(os.Getenv(environment.SlinkInstances)),
	})
	if len(service.apis) == 0 && len(service.slink) == 0 {
		return
	}

	DefaultService = service
	budgets := []any{}
	for _, provider := range service.apis {
		budgets = append(budgets, provider.name+"PerHour", provider.perHour)
	}
	slog.Info("GIFs: search enabled", append([]any{"apis", service.APIProviders(), "slink", service.SlinkHosts()}, budgets...)...)
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

func New(options Options) *Service {
	rating := strings.ToLower(strings.TrimSpace(options.Rating))
	switch rating {
	case "g", "pg", "pg-13", "r":
	default:
		rating = "pg-13"
	}

	s := &Service{
		rating: rating,
		slink:  options.Slink,
		client: &http.Client{Timeout: requestTimeout},
		now:    time.Now,
		cache:  map[string]cacheEntry{},
	}
	if options.GiphyKey != "" {
		s.apis = append(s.apis, &apiProvider{
			name:    ProviderGiphy,
			key:     options.GiphyKey,
			perHour: positiveOr(options.GiphyPerHour, defaultGiphyPerHour),
			baseURL: "https://api.giphy.com",
			hosts:   []string{"*.giphy.com"},
			search:  s.searchGiphy,
		})
	}
	if options.KlipyKey != "" {
		s.apis = append(s.apis, &apiProvider{
			name:    ProviderKlipy,
			key:     options.KlipyKey,
			perHour: positiveOr(options.KlipyPerHour, defaultKlipyPerHour),
			baseURL: "https://api.klipy.com",
			// Not *.klipy.com, links to klipy.com pages must stay links
			hosts:  []string{"static.klipy.com"},
			search: s.searchKlipy,
		})
	}
	return s
}

func positiveOr(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

// The configured API providers (giphy, klipy), searched when a viewer presses enter
func (s *Service) APIProviders() []string {
	names := []string{}
	for _, provider := range s.apis {
		names = append(names, provider.name)
	}
	return names
}

func (s *Service) SlinkHosts() (hosts []string) {
	for _, instance := range s.slink {
		hosts = append(hosts, instance.host)
	}
	return hosts
}

// Hosts GIFs are served from, allowed in chat automatically
func (s *Service) Hosts() []string {
	hosts := s.SlinkHosts()
	for _, provider := range s.apis {
		hosts = append(hosts, provider.hosts...)
	}
	return hosts
}

// Search searches the Slink instances, and Giphy and KLIPY when includeAPIs
// is set. An empty query lists the newest images of the Slink instances; the
// API providers are never searched without a query, to save their budget.
func (s *Service) Search(ctx context.Context, query string, includeAPIs bool) SearchResult {
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
		lists   = make([][]GIF, len(s.slink)+len(s.apis))
		limited []string
	)
	for i, instance := range s.slink {
		wait.Go(func() {
			lists[i] = s.cached("slink:"+instance.host+":"+strings.ToLower(query), slinkCacheDuration, func() ([]GIF, error) {
				return s.searchSlink(fetchContext, instance, query)
			})
		})
	}
	for i, provider := range s.apis {
		if !includeAPIs || query == "" {
			break
		}
		wait.Go(func() {
			lists[len(s.slink)+i] = s.cached(provider.name+":"+strings.ToLower(query), apiCacheDuration, func() ([]GIF, error) {
				if !s.takeRequest(provider) {
					lock.Lock()
					limited = append(limited, provider.name)
					lock.Unlock()
					return nil, errBudgetUsedUp
				}
				return provider.search(fetchContext, provider, query)
			})
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
	slices.Sort(limited)
	result.Limited = limited
	return result
}

var errBudgetUsedUp = fmt.Errorf("hourly request budget used up")

// Keeps a provider under its hourly request limit, shared by all viewers
func (s *Service) takeRequest(provider *apiProvider) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	hourAgo := s.now().Add(-time.Hour)
	recent := provider.requests[:0]
	for _, at := range provider.requests {
		if at.After(hourAgo) {
			recent = append(recent, at)
		}
	}
	provider.requests = recent

	if len(provider.requests) >= provider.perHour {
		return false
	}
	provider.requests = append(provider.requests, s.now())
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
		if err != errBudgetUsedUp {
			slog.Error("GIFs: search failed", "search", key, "err", err)
		} else {
			slog.Warn("GIFs: hourly search budget used up, showing the other results only", "search", key)
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
	defer func() { _ = response.Body.Close() }()

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

func (s *Service) searchGiphy(ctx context.Context, provider *apiProvider, query string) ([]GIF, error) {
	params := url.Values{
		"api_key": {provider.key},
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
	if err := s.getJSON(ctx, provider.baseURL+"/v1/gifs/search?"+params.Encode(), &response); err != nil {
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
		gif := GIF{
			URL:      stripQuery(full.URL),
			Preview:  stripQuery(preview),
			Width:    atoi(full.Width),
			Height:   atoi(full.Height),
			Title:    item.Title,
			Provider: ProviderGiphy,
			Source:   "giphy.com",
		}
		gifs = appendFromHosts(gifs, gif, provider)
	}
	return gifs, nil
}

// KLIPY's search is a drop-in replacement of Tenor's v2 API
// (https://github.com/klipycom/Migrate-From-Tenor-To-Klipy)
func (s *Service) searchKlipy(ctx context.Context, provider *apiProvider, query string) ([]GIF, error) {
	params := url.Values{
		"key":           {provider.key},
		"client_key":    {"broadcast-box"},
		"q":             {query},
		"limit":         {strconv.Itoa(resultLimit)},
		"media_filter":  {"gif,mediumgif,tinygif"},
		"contentfilter": {map[string]string{"g": "high", "pg": "medium", "pg-13": "low", "r": "off"}[s.rating]},
	}

	type media struct {
		URL  string `json:"url"`
		Dims []int  `json:"dims"`
	}
	var response struct {
		Results []struct {
			Title              string           `json:"title"`
			ContentDescription string           `json:"content_description"`
			ItemURL            string           `json:"itemurl"`
			MediaFormats       map[string]media `json:"media_formats"`
		} `json:"results"`
	}
	if err := s.getJSON(ctx, provider.baseURL+"/v2/search?"+params.Encode(), &response); err != nil {
		return nil, err
	}

	var gifs []GIF
	for _, item := range response.Results {
		// Medium size keeps chat light, the full gif can be several MB
		full, ok := item.MediaFormats["mediumgif"]
		if !ok || full.URL == "" {
			full = item.MediaFormats["gif"]
		}
		preview, ok := item.MediaFormats["tinygif"]
		if !ok || preview.URL == "" {
			preview = full
		}
		title := item.Title
		if title == "" {
			title = item.ContentDescription
		}
		gif := GIF{URL: full.URL, Preview: preview.URL, Title: title, Provider: ProviderKlipy, Source: "klipy.com", pageURL: item.ItemURL}
		if len(full.Dims) == 2 {
			gif.Width, gif.Height = full.Dims[0], full.Dims[1]
		}
		gifs = appendFromHosts(gifs, gif, provider)
	}
	return gifs, nil
}

// Keeps GIFs served from the provider's own hosts, those are allowed in chat
func appendFromHosts(gifs []GIF, gif GIF, provider *apiProvider) []GIF {
	if IsAllowedURL(gif.URL, provider.hosts) && IsAllowedURL(gif.Preview, provider.hosts) {
		return append(gifs, gif)
	}
	if gif.URL != "" {
		slog.Warn("GIFs: result skipped, not served from the provider's known hosts", "provider", provider.name, "url", gif.URL, "hosts", provider.hosts)
	}
	return gifs
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
