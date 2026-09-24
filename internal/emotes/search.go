package emotes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	searchCacheDuration = 10 * time.Minute
	searchCacheSize     = 500
	searchLimit         = 50
	MinSearchLength     = 2
	maxSearchLength     = 50
)

// Search looks up emotes by name on 7TV, BetterTTV and FrankerFaceZ, so
// viewers can use emotes that aren't in the channel's sets. Results are
// cached per query.
func (s *Service) Search(ctx context.Context, query string) []Emote {
	query = strings.TrimSpace(query)
	if len([]rune(query)) < MinSearchLength || len(query) > maxSearchLength {
		return nil
	}
	key := strings.ToLower(query)

	s.searchLock.Lock()
	entry, ok := s.searchCache[key]
	s.searchLock.Unlock()
	if ok && time.Now().Before(entry.expires) {
		return entry.emotes
	}

	fetchContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), requestTimeout)
	defer cancel()

	var (
		wait    sync.WaitGroup
		lock    sync.Mutex
		results = map[string][]Emote{}
	)
	for _, provider := range s.providers {
		wait.Go(func() {
			found, err := s.searchProvider(fetchContext, provider, query)
			if err != nil {
				slog.Error("Emotes: search failed", "provider", provider, "query", query, "err", err)
				return
			}
			lock.Lock()
			results[provider] = found
			lock.Unlock()
		})
	}
	wait.Wait()

	// Interleave providers so every provider's best matches show up first
	var merged []Emote
	seen := map[string]bool{}
	for i := 0; i < searchLimit; i++ {
		added := false
		for _, provider := range s.providers {
			if i < len(results[provider]) {
				added = true
				emote := results[provider][i]
				if key := emote.Provider + ":" + emote.URL; !seen[key] {
					seen[key] = true
					merged = append(merged, emote)
				}
			}
		}
		if !added {
			break
		}
	}

	s.searchLock.Lock()
	if len(s.searchCache) >= searchCacheSize {
		for cached, entry := range s.searchCache {
			if time.Now().After(entry.expires) || len(s.searchCache) >= searchCacheSize {
				delete(s.searchCache, cached)
			}
		}
	}
	s.searchCache[key] = cacheEntry{emotes: merged, expires: time.Now().Add(searchCacheDuration)}
	s.searchLock.Unlock()

	return merged
}

func (s *Service) searchProvider(ctx context.Context, provider, query string) ([]Emote, error) {
	switch provider {
	case Provider7TV:
		return s.search7TV(ctx, query)
	case ProviderBTTV:
		// BetterTTV only searches queries of 3 or more characters
		if len([]rune(query)) < 3 {
			return nil, nil
		}
		var emotes []bttvEmote
		err := s.getJSON(ctx, provider, "/3/emotes/shared/search?offset=0&limit="+fmt.Sprint(searchLimit)+"&query="+url.QueryEscape(query), &emotes)
		return bttvEmotes(emotes), err
	case ProviderFFZ:
		var response struct {
			Emoticons []struct {
				Name     string            `json:"name"`
				URLs     map[string]string `json:"urls"`
				Animated map[string]string `json:"animated"`
			} `json:"emoticons"`
		}
		err := s.getJSON(ctx, provider, "/v1/emotes?sort=count-desc&page=1&per_page="+fmt.Sprint(searchLimit)+"&q="+url.QueryEscape(query), &response)
		return ffzSet{Emoticons: response.Emoticons}.emotes(), err
	}
	return nil, nil
}

const sevenTVSearchQuery = `query SearchEmotes($query: String!, $page: Int, $limit: Int) {
	emotes(query: $query, page: $page, limit: $limit) {
		items { id name animated host { url files { name } } }
	}
}`

func (s *Service) search7TV(ctx context.Context, query string) ([]Emote, error) {
	body, _ := json.Marshal(map[string]any{
		"operationName": "SearchEmotes",
		"query":         sevenTVSearchQuery,
		"variables":     map[string]any{"query": query, "page": 1, "limit": searchLimit},
	})

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURLs[Provider7TV]+"/v3/gql", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := s.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("7tv search returned %d", response.StatusCode)
	}

	var result struct {
		Data struct {
			Emotes struct {
				Items []struct {
					Name     string      `json:"name"`
					Animated bool        `json:"animated"`
					Host     sevenTVHost `json:"host"`
				} `json:"items"`
			} `json:"emotes"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, response.Body, maxResponseBytes)).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Errors) > 0 && len(result.Data.Emotes.Items) == 0 {
		return nil, fmt.Errorf("7tv search: %s", result.Errors[0].Message)
	}

	var emotes []Emote
	for _, item := range result.Data.Emotes.Items {
		emotes = appendEmote(emotes, sevenTVEmote(item.Name, sevenTVEmoteData{Animated: item.Animated, Host: item.Host}))
	}
	return emotes, nil
}
