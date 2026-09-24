// Package emotes collects third party chat emotes (Twitch, 7TV, BetterTTV
// and FrankerFaceZ) for a stream. Lists are fetched and cached by the server so
// viewers download one merged list instead of every viewer calling every
// provider.
package emotes

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/glimesh/broadcast-box/internal/environment"
)

const (
	cacheDuration      = 30 * time.Minute
	errorRetryInterval = 2 * time.Minute
	requestTimeout     = 10 * time.Second
	maxResponseBytes   = 8 << 20

	Provider7TV    = "7tv"
	ProviderBTTV   = "bttv"
	ProviderFFZ    = "ffz"
	ProviderTwitch = "twitch"
)

// Emote images are only accepted from the providers' own CDNs
var allowedImageHosts = map[string]bool{
	"cdn.7tv.app":          true,
	"cdn.betterttv.net":    true,
	"cdn.frankerfacez.com": true,
	"static-cdn.jtvnw.net": true,
}

var validEmoteCode = regexp.MustCompile(`^\S{1,100}$`)

type Emote struct {
	Code     string `json:"code"`
	URL      string `json:"url"`
	URL2x    string `json:"url2x,omitempty"`
	Provider string `json:"provider"`
	Animated bool   `json:"animated,omitempty"`
}

type cacheEntry struct {
	emotes  []Emote
	expires time.Time
}

type Service struct {
	providers []string
	// Twitch user ID per stream key, "" applies to every stream
	twitchIDs map[string]string

	client *http.Client
	// Provider API base URLs, replaced in tests
	baseURLs map[string]string

	twitch *twitchClient

	lock  sync.Mutex
	cache map[string]cacheEntry

	searchLock  sync.Mutex
	searchCache map[string]cacheEntry
}

// DefaultService is nil when no providers are configured
var DefaultService *Service

// Setup reads CHAT_EMOTE_PROVIDERS (e.g. "7tv,bttv,ffz") and
// CHAT_EMOTES_TWITCH_IDS ("<twitchID>" or "<streamKey>:<twitchID>,...").
// Twitch's own emotes are added when TWITCH_CLIENT_ID and
// TWITCH_CLIENT_SECRET are set.
func Setup() {
	var providers []string
	for provider := range strings.SplitSeq(strings.ToLower(os.Getenv(environment.ChatEmoteProviders)), ",") {
		switch provider = strings.TrimSpace(provider); provider {
		case Provider7TV, ProviderBTTV, ProviderFFZ, ProviderTwitch:
			if !slices.Contains(providers, provider) {
				providers = append(providers, provider)
			}
		case "":
		default:
			slog.Error("Emotes: unknown provider ignored", "provider", provider)
		}
	}

	clientID, clientSecret := os.Getenv(environment.TwitchClientID), os.Getenv(environment.TwitchClientSecret)
	hasTwitchCredentials := clientID != "" && clientSecret != ""
	if hasTwitchCredentials && !slices.Contains(providers, ProviderTwitch) {
		providers = append([]string{ProviderTwitch}, providers...)
	}
	if !hasTwitchCredentials && slices.Contains(providers, ProviderTwitch) {
		slog.Error("Emotes: Twitch emotes need TWITCH_CLIENT_ID and TWITCH_CLIENT_SECRET")
		providers = slices.DeleteFunc(providers, func(provider string) bool { return provider == ProviderTwitch })
	}
	if len(providers) == 0 {
		return
	}

	DefaultService = New(providers, ParseTwitchIDs(os.Getenv(environment.ChatEmotesTwitchIDs)))
	if hasTwitchCredentials {
		DefaultService.twitch = newTwitchClient(DefaultService.client, clientID, clientSecret)
	}
	slog.Info("Emotes: enabled", "providers", providers, "twitchIDs", DefaultService.twitchIDs)
}

func ParseTwitchIDs(value string) map[string]string {
	ids := map[string]string{}
	for entry := range strings.SplitSeq(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		streamKey, id, found := strings.Cut(entry, ":")
		if !found {
			streamKey, id = "", entry
		}
		ids[strings.TrimSpace(streamKey)] = strings.TrimSpace(id)
	}
	return ids
}

func New(providers []string, twitchIDs map[string]string) *Service {
	return &Service{
		providers: providers,
		twitchIDs: twitchIDs,
		client:    &http.Client{Timeout: requestTimeout},
		baseURLs: map[string]string{
			Provider7TV:    "https://7tv.io",
			ProviderBTTV:   "https://api.betterttv.net",
			ProviderFFZ:    "https://api.frankerfacez.com",
			ProviderTwitch: "https://api.twitch.tv",
		},
		cache:       map[string]cacheEntry{},
		searchCache: map[string]cacheEntry{},
	}
}

// ForStream returns channel emotes followed by global emotes. When two
// emotes share a code, channel emotes win, then 7TV, BetterTTV, FrankerFaceZ.
func (s *Service) ForStream(ctx context.Context, streamKey string) []Emote {
	twitchID, ok := s.twitchIDs[streamKey]
	if !ok {
		twitchID = s.twitchIDs[""]
	}

	var lists [][]Emote
	if twitchID != "" {
		for _, provider := range s.providers {
			lists = append(lists, s.cached(ctx, provider+":channel:"+twitchID, func(ctx context.Context) ([]Emote, error) {
				return s.fetchChannel(ctx, provider, twitchID)
			}))
		}
	}
	for _, provider := range s.providers {
		lists = append(lists, s.cached(ctx, provider+":global", func(ctx context.Context) ([]Emote, error) {
			return s.fetchGlobal(ctx, provider)
		}))
	}

	seen := map[string]bool{}
	var merged []Emote
	for _, list := range lists {
		for _, emote := range list {
			if !seen[emote.Code] {
				seen[emote.Code] = true
				merged = append(merged, emote)
			}
		}
	}
	return merged
}

func (s *Service) cached(ctx context.Context, key string, fetch func(context.Context) ([]Emote, error)) []Emote {
	s.lock.Lock()
	entry, ok := s.cache[key]
	s.lock.Unlock()
	if ok && time.Now().Before(entry.expires) {
		return entry.emotes
	}

	// Don't let a viewer closing the page abort a fetch everyone shares
	fetchContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*requestTimeout)
	defer cancel()

	emotes, err := fetch(fetchContext)
	expires := time.Now().Add(cacheDuration)
	if err != nil {
		slog.Error("Emotes: fetching failed", "list", key, "err", err)
		// Keep serving the previous list, and don't hammer a failing provider
		emotes = entry.emotes
		expires = time.Now().Add(errorRetryInterval)
	} else {
		slog.Info("Emotes: loaded", "list", key, "count", len(emotes))
	}

	s.lock.Lock()
	s.cache[key] = cacheEntry{emotes: emotes, expires: expires}
	s.lock.Unlock()
	return emotes
}

func (s *Service) getJSON(ctx context.Context, provider, path string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURLs[provider]+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")

	response, err := s.client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode == http.StatusNotFound {
		slog.Info("Emotes: not found at provider, the channel may not use it", "provider", provider, "path", path)
		return nil
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s returned %d", path, response.StatusCode)
	}
	return json.NewDecoder(http.MaxBytesReader(nil, response.Body, maxResponseBytes)).Decode(target)
}

func (s *Service) fetchGlobal(ctx context.Context, provider string) ([]Emote, error) {
	switch provider {
	case Provider7TV:
		var set sevenTVEmoteSet
		err := s.getJSON(ctx, provider, "/v3/emote-sets/global", &set)
		return set.emotes(), err
	case ProviderBTTV:
		var emotes []bttvEmote
		err := s.getJSON(ctx, provider, "/3/cached/emotes/global", &emotes)
		return bttvEmotes(emotes), err
	case ProviderTwitch:
		return s.fetchTwitch(ctx, "/helix/chat/emotes/global")
	case ProviderFFZ:
		var response ffzGlobalResponse
		err := s.getJSON(ctx, provider, "/v1/set/global", &response)
		var emotes []Emote
		for _, id := range response.DefaultSets {
			emotes = append(emotes, response.Sets[fmt.Sprint(id)].emotes()...)
		}
		return emotes, err
	}
	return nil, nil
}

func (s *Service) fetchChannel(ctx context.Context, provider, twitchID string) ([]Emote, error) {
	id := url.PathEscape(twitchID)
	switch provider {
	case ProviderTwitch:
		return s.fetchTwitch(ctx, "/helix/chat/emotes?broadcaster_id="+url.QueryEscape(twitchID))
	case Provider7TV:
		var user struct {
			EmoteSet sevenTVEmoteSet `json:"emote_set"`
		}
		err := s.getJSON(ctx, provider, "/v3/users/twitch/"+id, &user)
		return user.EmoteSet.emotes(), err
	case ProviderBTTV:
		var user struct {
			ChannelEmotes []bttvEmote `json:"channelEmotes"`
			SharedEmotes  []bttvEmote `json:"sharedEmotes"`
		}
		err := s.getJSON(ctx, provider, "/3/cached/users/twitch/"+id, &user)
		return bttvEmotes(append(user.ChannelEmotes, user.SharedEmotes...)), err
	case ProviderFFZ:
		var response struct {
			Room struct {
				Set int `json:"set"`
			} `json:"room"`
			Sets map[string]ffzSet `json:"sets"`
		}
		err := s.getJSON(ctx, provider, "/v1/room/id/"+id, &response)
		return response.Sets[fmt.Sprint(response.Room.Set)].emotes(), err
	}
	return nil, nil
}

// Provider response formats

type sevenTVHost struct {
	URL   string `json:"url"`
	Files []struct {
		Name string `json:"name"`
	} `json:"files"`
}

type sevenTVEmoteData struct {
	Animated bool        `json:"animated"`
	Host     sevenTVHost `json:"host"`
}

type sevenTVEmoteSet struct {
	Emotes []struct {
		Name string           `json:"name"`
		Data sevenTVEmoteData `json:"data"`
	} `json:"emotes"`
}

func (set sevenTVEmoteSet) emotes() (emotes []Emote) {
	for _, emote := range set.Emotes {
		emotes = appendEmote(emotes, sevenTVEmote(emote.Name, emote.Data))
	}
	return emotes
}

func sevenTVEmote(name string, data sevenTVEmoteData) Emote {
	files := map[string]bool{}
	for _, file := range data.Host.Files {
		files[file.Name] = true
	}
	file1x, file2x := "1x.webp", "2x.webp"
	if !files[file1x] && files["1x.avif"] {
		file1x, file2x = "1x.avif", "2x.avif"
	}

	base := absoluteURL(data.Host.URL)
	url2x := ""
	if files[file2x] {
		url2x = base + "/" + file2x
	}
	return Emote{
		Code:     name,
		URL:      base + "/" + file1x,
		URL2x:    url2x,
		Provider: Provider7TV,
		Animated: data.Animated,
	}
}

type bttvEmote struct {
	ID       string `json:"id"`
	Code     string `json:"code"`
	Animated bool   `json:"animated"`
}

func bttvEmotes(list []bttvEmote) (emotes []Emote) {
	for _, emote := range list {
		base := "https://cdn.betterttv.net/emote/" + url.PathEscape(emote.ID)
		emotes = appendEmote(emotes, Emote{
			Code:     emote.Code,
			URL:      base + "/1x",
			URL2x:    base + "/2x",
			Provider: ProviderBTTV,
			Animated: emote.Animated,
		})
	}
	return emotes
}

type ffzSet struct {
	Emoticons []struct {
		Name     string            `json:"name"`
		URLs     map[string]string `json:"urls"`
		Animated map[string]string `json:"animated"`
	} `json:"emoticons"`
}

type ffzGlobalResponse struct {
	DefaultSets []int             `json:"default_sets"`
	Sets        map[string]ffzSet `json:"sets"`
}

func (set ffzSet) emotes() (emotes []Emote) {
	for _, emote := range set.Emoticons {
		urls := emote.URLs
		if len(emote.Animated) > 0 {
			urls = emote.Animated
		}
		emotes = appendEmote(emotes, Emote{
			Code:     emote.Name,
			URL:      absoluteURL(urls["1"]),
			URL2x:    absoluteURL(urls["2"]),
			Provider: ProviderFFZ,
			Animated: len(emote.Animated) > 0,
		})
	}
	return emotes
}

func absoluteURL(value string) string {
	if strings.HasPrefix(value, "//") {
		return "https:" + value
	}
	return value
}

// IsAllowedImageURL reports whether an emote image URL points at one of the
// providers' CDNs
func IsAllowedImageURL(value string) bool {
	return isAllowedImage(value)
}

// IsValidCode reports whether an emote code is acceptable in chat
func IsValidCode(code string) bool {
	return validEmoteCode.MatchString(code)
}

func isAllowedImage(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && allowedImageHosts[parsed.Host]
}

func appendEmote(emotes []Emote, emote Emote) []Emote {
	if !validEmoteCode.MatchString(emote.Code) || !isAllowedImage(emote.URL) {
		return emotes
	}
	if emote.URL2x != "" && !isAllowedImage(emote.URL2x) {
		emote.URL2x = ""
	}
	return append(emotes, emote)
}
