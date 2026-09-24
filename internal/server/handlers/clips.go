package handlers

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"

	"github.com/glimesh/broadcast-box/internal/clips"
	"github.com/glimesh/broadcast-box/internal/environment"
	"github.com/glimesh/broadcast-box/internal/server/authorization"
	"github.com/glimesh/broadcast-box/internal/server/helpers"
	"github.com/glimesh/broadcast-box/internal/webrtc/sessions/manager"
)

// Routes:
//
//	GET    /api/clips/config                      clip settings, {"enabled": false} when disabled
//	GET    /api/clips?key=<streamKey>             published clips, newest first
//	POST   /api/clips/drafts?key=<streamKey>      snapshot the clip buffer into a draft
//	GET    /api/clips/drafts/<id>                 draft video for the editor
//	POST   /api/clips/drafts/<id>/publish         {"start","end","title"} -> clip
//	GET    /api/clips/<streamKey>/<id>[?download] clip video
//	DELETE /api/clips/<streamKey>/<id>            admin or stream profile token
func clipsHandler(responseWriter http.ResponseWriter, request *http.Request) {
	service := clips.DefaultService
	path := strings.Trim(strings.TrimPrefix(request.URL.Path, "/api/clips"), "/")
	segments := strings.Split(path, "/")

	if path == "config" {
		clipsConfigHandler(responseWriter, service)
		return
	}
	if service == nil {
		helpers.LogHTTPError(responseWriter, clips.ErrDisabled.Error(), http.StatusNotFound)
		return
	}

	switch {
	case path == "" && request.Method == http.MethodGet:
		list, err := service.List(request.Context(), request.URL.Query().Get("key"))
		if err != nil {
			writeClipError(responseWriter, err)
			return
		}
		writeJSON(responseWriter, list)

	case path == "drafts" && request.Method == http.MethodPost:
		createDraftHandler(responseWriter, request, service)

	case len(segments) == 2 && segments[0] == "drafts" && request.Method == http.MethodGet:
		service.ServeDraft(responseWriter, request, segments[1])

	case len(segments) == 3 && segments[0] == "drafts" && segments[2] == "publish" && request.Method == http.MethodPost:
		publishClipHandler(responseWriter, request, service, segments[1])

	case len(segments) == 2 && request.Method == http.MethodGet:
		_, download := request.URL.Query()["download"]
		service.Serve(responseWriter, request, segments[0], strings.TrimSuffix(segments[1], ".mkv"), download)

	case len(segments) == 2 && request.Method == http.MethodDelete:
		if !canManageClips(request, segments[0]) {
			helpers.LogHTTPError(responseWriter, "Unauthorized", http.StatusUnauthorized)
			return
		}
		if err := service.Delete(request.Context(), segments[0], segments[1]); err != nil {
			writeClipError(responseWriter, err)
			return
		}
		responseWriter.WriteHeader(http.StatusNoContent)

	default:
		helpers.LogHTTPError(responseWriter, "Not found", http.StatusNotFound)
	}
}

func clipsConfigHandler(responseWriter http.ResponseWriter, service *clips.Service) {
	if service == nil {
		writeJSON(responseWriter, map[string]any{"enabled": false})
		return
	}

	config := service.Config()
	writeJSON(responseWriter, map[string]any{
		"enabled":        true,
		"bufferSeconds":  config.BufferDuration.Seconds(),
		"maxClipSeconds": config.MaxClipDuration.Seconds(),
	})
}

func createDraftHandler(responseWriter http.ResponseWriter, request *http.Request, service *clips.Service) {
	if !service.Allow(clientAddress(request)) {
		writeClipError(responseWriter, clips.ErrRateLimited)
		return
	}

	streamKey := request.URL.Query().Get("key")
	session, ok := manager.SessionsManager.GetSessionByID(streamKey)
	if !ok || session.Recorder == nil {
		writeClipError(responseWriter, clips.ErrNothingRecorded)
		return
	}

	draft, err := service.CreateDraft(streamKey, session.Recorder)
	if err != nil {
		writeClipError(responseWriter, err)
		return
	}
	writeJSON(responseWriter, draft)
}

func publishClipHandler(responseWriter http.ResponseWriter, request *http.Request, service *clips.Service, draftID string) {
	if !service.Allow(clientAddress(request)) {
		writeClipError(responseWriter, clips.ErrRateLimited)
		return
	}

	var body struct {
		Start float64 `json:"start"`
		End   float64 `json:"end"`
		Title string  `json:"title"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(responseWriter, request.Body, 4096)).Decode(&body); err != nil {
		helpers.LogHTTPError(responseWriter, "Invalid request", http.StatusBadRequest)
		return
	}

	clip, err := service.Publish(request.Context(), draftID, body.Start, body.End, body.Title)
	if err != nil {
		writeClipError(responseWriter, err)
		return
	}
	writeJSON(responseWriter, clip)
}

// The admin token, or the token of the stream's own profile
func canManageClips(request *http.Request, streamKey string) bool {
	token := helpers.ResolveBearerToken(request.Header.Get("Authorization"))
	if token == "" {
		return false
	}

	if adminToken := os.Getenv(environment.FrontendAdminToken); adminToken != "" &&
		subtle.ConstantTimeCompare([]byte(adminToken), []byte(token)) == 1 {
		return true
	}

	profile, err := authorization.GetPublicProfile(token)
	return err == nil && profile != nil && profile.StreamKey == streamKey
}

func clientAddress(request *http.Request) string {
	if forwarded := request.Header.Get("X-Forwarded-For"); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		return strings.TrimSpace(first)
	}
	if realIP := request.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return request.RemoteAddr
	}
	return host
}

func writeClipError(responseWriter http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, clips.ErrRateLimited):
		status = http.StatusTooManyRequests
	case errors.Is(err, clips.ErrDraftNotFound), errors.Is(err, clips.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, clips.ErrNothingRecorded), errors.Is(err, clips.ErrUnsupportedCodec),
		errors.Is(err, clips.ErrInvalidRange), errors.Is(err, clips.ErrInvalidKey):
		status = http.StatusBadRequest
	default:
		slog.Error("Clips: request failed", "err", err)
	}
	helpers.LogHTTPError(responseWriter, err.Error(), status)
}

func writeJSON(responseWriter http.ResponseWriter, value any) {
	responseWriter.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(responseWriter).Encode(value); err != nil {
		slog.Error("API: encoding response failed", "err", err)
	}
}
