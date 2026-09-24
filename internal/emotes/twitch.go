package emotes

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// Twitch emotes need an app access token (client credentials flow)
type twitchClient struct {
	client       *http.Client
	clientID     string
	clientSecret string
	tokenURL     string

	lock    sync.Mutex
	token   string
	expires time.Time
}

func newTwitchClient(client *http.Client, clientID, clientSecret string) *twitchClient {
	return &twitchClient{
		client:       client,
		clientID:     clientID,
		clientSecret: clientSecret,
		tokenURL:     "https://id.twitch.tv/oauth2/token",
	}
}

func (t *twitchClient) accessToken(ctx context.Context) (string, error) {
	t.lock.Lock()
	defer t.lock.Unlock()

	if t.token != "" && time.Now().Before(t.expires) {
		return t.token, nil
	}

	form := url.Values{
		"client_id":     {t.clientID},
		"client_secret": {t.clientSecret},
		"grant_type":    {"client_credentials"},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, t.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := t.client.Do(request)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("twitch token request returned %d, check TWITCH_CLIENT_ID and TWITCH_CLIENT_SECRET", response.StatusCode)
	}

	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return "", err
	}

	t.token = body.AccessToken
	// Renew a little before it expires
	t.expires = time.Now().Add(time.Duration(body.ExpiresIn)*time.Second - time.Minute)
	return t.token, nil
}

type twitchEmotesResponse struct {
	Data []struct {
		ID     string   `json:"id"`
		Name   string   `json:"name"`
		Format []string `json:"format"`
	} `json:"data"`
	Template string `json:"template"`
}

func (s *Service) fetchTwitch(ctx context.Context, path string) ([]Emote, error) {
	if s.twitch == nil {
		return nil, nil
	}

	token, err := s.twitch.accessToken(ctx)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURLs[ProviderTwitch]+path, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Client-Id", s.twitch.clientID)
	request.Header.Set("Authorization", "Bearer "+token)

	response, err := s.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode == http.StatusUnauthorized {
		s.twitch.lock.Lock()
		s.twitch.token = ""
		s.twitch.lock.Unlock()
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("twitch %s returned %d", path, response.StatusCode)
	}

	var body twitchEmotesResponse
	if err := json.NewDecoder(http.MaxBytesReader(nil, response.Body, maxResponseBytes)).Decode(&body); err != nil {
		return nil, err
	}

	template := body.Template
	if template == "" {
		template = "https://static-cdn.jtvnw.net/emoticons/v2/{{id}}/{{format}}/{{theme_mode}}/{{scale}}"
	}

	var emotes []Emote
	for _, emote := range body.Data {
		format := "static"
		if slices.Contains(emote.Format, "animated") {
			format = "animated"
		}
		image := func(scale string) string {
			return strings.NewReplacer(
				"{{id}}", url.PathEscape(emote.ID),
				"{{format}}", format,
				"{{theme_mode}}", "dark",
				"{{scale}}", scale,
			).Replace(template)
		}
		emotes = appendEmote(emotes, Emote{
			Code:     emote.Name,
			URL:      image("1.0"),
			URL2x:    image("2.0"),
			Provider: ProviderTwitch,
			Animated: format == "animated",
		})
	}
	return emotes, nil
}
