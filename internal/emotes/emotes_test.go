package emotes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// Trimmed responses in the providers' formats
var fixtures = map[string]string{
	"/v3/emote-sets/global": `{"id":"global","emotes":[
		{"id":"a","name":"EZ","data":{"animated":false,"host":{"url":"//cdn.7tv.app/emote/a","files":[{"name":"1x.webp"},{"name":"2x.webp"}]}}},
		{"id":"b","name":"Clap","data":{"animated":true,"host":{"url":"//cdn.7tv.app/emote/b","files":[{"name":"1x.webp"}]}}},
		{"id":"c","name":"Evil","data":{"host":{"url":"//evil.example.com/emote/c","files":[{"name":"1x.webp"}]}}}
	]}`,
	"/v3/users/twitch/1234": `{"id":"user","emote_set":{"emotes":[
		{"id":"d","name":"Clap","data":{"animated":false,"host":{"url":"//cdn.7tv.app/emote/d","files":[{"name":"1x.webp"}]}}}
	]}}`,
	"/3/cached/emotes/global": `[{"id":"54fa925e01e468494b85b54d","code":"OhMyGoodness","imageType":"png","animated":false},
		{"id":"566ca04265dbbdab32ec054a","code":"EZ","imageType":"png","animated":false}]`,
	"/3/cached/users/twitch/1234": `{"id":"u","channelEmotes":[{"id":"ch1","code":"myEmote","imageType":"gif","animated":true}],
		"sharedEmotes":[{"id":"sh1","code":"catJAM","imageType":"gif","animated":true}]}`,
	"/v1/set/global": `{"default_sets":[3],"sets":{
		"3":{"id":3,"emoticons":[{"id":1,"name":"ZreknarF","urls":{"1":"https://cdn.frankerfacez.com/emote/1/1","2":"https://cdn.frankerfacez.com/emote/1/2"}}]},
		"4":{"id":4,"emoticons":[{"id":2,"name":"NotDefault","urls":{"1":"https://cdn.frankerfacez.com/emote/2/1"}}]}
	}}`,
	"/v1/room/id/1234": `{"room":{"twitch_id":1234,"set":999},"sets":{"999":{"emoticons":[
		{"id":5,"name":"roomEmote","urls":{"1":"//cdn.frankerfacez.com/emote/5/1"},"animated":{"1":"https://cdn.frankerfacez.com/emote/5/animated/1"}}
	]}}}`,
}

func TestForStreamMergesProviders(t *testing.T) {
	requests := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests[r.URL.Path]++
		body, ok := fixtures[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	service := New([]string{Provider7TV, ProviderBTTV, ProviderFFZ}, ParseTwitchIDs("mystream:1234"))
	for provider := range service.baseURLs {
		service.baseURLs[provider] = server.URL
	}

	list := service.ForStream(context.Background(), "mystream")
	byCode := map[string]Emote{}
	var codes []string
	for _, emote := range list {
		byCode[emote.Code] = emote
		codes = append(codes, emote.Code)
	}

	// Channel emotes first, duplicates resolved in favour of channel then 7TV
	require.Equal(t, []string{"Clap", "myEmote", "catJAM", "roomEmote", "EZ", "OhMyGoodness", "ZreknarF"}, codes)
	require.Equal(t, "https://cdn.7tv.app/emote/d/1x.webp", byCode["Clap"].URL)
	require.Equal(t, Provider7TV, byCode["EZ"].Provider)
	require.Equal(t, "https://cdn.7tv.app/emote/a/2x.webp", byCode["EZ"].URL2x)
	require.Equal(t, "https://cdn.betterttv.net/emote/ch1/1x", byCode["myEmote"].URL)
	require.True(t, byCode["myEmote"].Animated)
	require.Equal(t, "https://cdn.frankerfacez.com/emote/5/animated/1", byCode["roomEmote"].URL)
	require.NotContains(t, byCode, "Evil", "images from unknown hosts are dropped")
	require.NotContains(t, byCode, "NotDefault", "only FFZ default sets are global")

	// Streams without a configured Twitch ID only get global emotes
	globalCodes := []string{}
	for _, emote := range service.ForStream(context.Background(), "other") {
		globalCodes = append(globalCodes, emote.Code)
	}
	require.Equal(t, []string{"EZ", "Clap", "OhMyGoodness", "ZreknarF"}, globalCodes)

	// Everything is cached
	service.ForStream(context.Background(), "mystream")
	for path, count := range requests {
		require.Equal(t, 1, count, path)
	}
}

func TestParseTwitchIDs(t *testing.T) {
	require.Equal(t, map[string]string{"": "42"}, ParseTwitchIDs("42"))
	require.Equal(t, map[string]string{"a": "1", "b": "2"}, ParseTwitchIDs(" a:1 , b:2 "))
}

func TestTwitchEmotes(t *testing.T) {
	tokenRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			tokenRequests++
			require.NoError(t, r.ParseForm())
			require.Equal(t, "client", r.PostForm.Get("client_id"))
			require.Equal(t, "client_credentials", r.PostForm.Get("grant_type"))
			_, _ = w.Write([]byte(`{"access_token":"token123","expires_in":3600,"token_type":"bearer"}`))
		case "/helix/chat/emotes":
			require.Equal(t, "781056551", r.URL.Query().Get("broadcaster_id"))
			require.Equal(t, "Bearer token123", r.Header.Get("Authorization"))
			require.Equal(t, "client", r.Header.Get("Client-Id"))
			_, _ = w.Write([]byte(`{"data":[{"id":"emotesv2_abc","name":"khyretHype","format":["static","animated"]}],
				"template":"https://static-cdn.jtvnw.net/emoticons/v2/{{id}}/{{format}}/{{theme_mode}}/{{scale}}"}`))
		case "/helix/chat/emotes/global":
			_, _ = w.Write([]byte(`{"data":[{"id":"25","name":"Kappa","format":["static"]}],
				"template":"https://static-cdn.jtvnw.net/emoticons/v2/{{id}}/{{format}}/{{theme_mode}}/{{scale}}"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := New([]string{ProviderTwitch}, ParseTwitchIDs("781056551"))
	service.baseURLs[ProviderTwitch] = server.URL
	service.twitch = newTwitchClient(service.client, "client", "secret")
	service.twitch.tokenURL = server.URL + "/oauth2/token"

	list := service.ForStream(context.Background(), "any-stream")
	require.Len(t, list, 2)
	require.Equal(t, Emote{
		Code:     "khyretHype",
		URL:      "https://static-cdn.jtvnw.net/emoticons/v2/emotesv2_abc/animated/dark/1.0",
		URL2x:    "https://static-cdn.jtvnw.net/emoticons/v2/emotesv2_abc/animated/dark/2.0",
		Provider: ProviderTwitch,
		Animated: true,
	}, list[0])
	require.Equal(t, "Kappa", list[1].Code)
	require.Equal(t, "https://static-cdn.jtvnw.net/emoticons/v2/25/static/dark/1.0", list[1].URL)
	require.Equal(t, 1, tokenRequests, "the token is reused")
}

func TestSearch(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Path {
		case "/v3/gql":
			var body struct {
				Variables struct {
					Query string `json:"query"`
				} `json:"variables"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Equal(t, "pepe", body.Variables.Query)
			_, _ = w.Write([]byte(`{"data":{"emotes":{"items":[
				{"id":"1","name":"pepeD","animated":true,"host":{"url":"//cdn.7tv.app/emote/1","files":[{"name":"1x.webp"},{"name":"2x.webp"}]}},
				{"id":"2","name":"pepeLaugh","animated":false,"host":{"url":"//cdn.7tv.app/emote/2","files":[{"name":"1x.webp"}]}}]}}}`))
		case "/3/emotes/shared/search":
			require.Equal(t, "pepe", r.URL.Query().Get("query"))
			_, _ = w.Write([]byte(`[{"id":"b1","code":"pepeJAM","animated":true}]`))
		case "/v1/emotes":
			require.Equal(t, "pepe", r.URL.Query().Get("q"))
			_, _ = w.Write([]byte(`{"emoticons":[{"id":9,"name":"PepeHands","urls":{"1":"https://cdn.frankerfacez.com/emote/9/1"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	service := New([]string{Provider7TV, ProviderBTTV, ProviderFFZ}, nil)
	for provider := range service.baseURLs {
		service.baseURLs[provider] = server.URL
	}

	var codes []string
	for _, emote := range service.Search(context.Background(), "pepe") {
		codes = append(codes, emote.Code)
	}
	// Interleaved: first result of every provider, then the second...
	require.Equal(t, []string{"pepeD", "pepeJAM", "PepeHands", "pepeLaugh"}, codes)

	service.Search(context.Background(), "PEPE")
	require.Equal(t, 3, requests, "searches are cached case insensitively")
	require.Empty(t, service.Search(context.Background(), "p"), "queries need 2 characters")
}
