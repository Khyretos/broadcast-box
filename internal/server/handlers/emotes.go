package handlers

import (
	"net/http"

	"github.com/glimesh/broadcast-box/internal/emotes"
)

// GET /api/chat/emotes?key=<streamKey> lists the 7TV, BetterTTV and
// FrankerFaceZ emotes for a stream, empty when none are configured
func emotesHandler(responseWriter http.ResponseWriter, request *http.Request) {
	list := []emotes.Emote{}
	if emotes.DefaultService != nil {
		if found := emotes.DefaultService.ForStream(request.Context(), request.URL.Query().Get("key")); found != nil {
			list = found
		}
	}

	responseWriter.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(responseWriter, map[string]any{"emotes": list})
}
