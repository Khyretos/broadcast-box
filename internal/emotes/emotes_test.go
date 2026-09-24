package emotes

import (
	"context"
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
