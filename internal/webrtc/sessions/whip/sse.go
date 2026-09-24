package whip

import (
	"encoding/json"
	"log/slog"
	"math"
	"slices"
	"strings"
)

// Returns all available Video and Audio layers of the provided stream key
func (w *WHIPSession) GetAvailableLayersEvent() string {
	videoLayers := []simulcastLayerResponse{}
	audioLayers := []simulcastLayerResponse{}

	w.TracksLock.RLock()

	// Add available video layers
	for _, track := range w.VideoTracks {
		videoLayers = append(videoLayers, simulcastLayerResponse{
			EncodingID:      track.Rid,
			Width:           track.Width.Load(),
			Height:          track.Height.Load(),
			FramesPerSecond: math.Round(float64(track.FramesPerSecond.Load())) / 100,
			Bitrate:         track.Bitrate.Load() * 8,
			priority:        track.Priority,
		})
	}

	// Best layer first
	slices.SortFunc(videoLayers, func(a, b simulcastLayerResponse) int {
		if a.priority != b.priority {
			return a.priority - b.priority
		}
		return strings.Compare(a.EncodingID, b.EncodingID)
	})

	// Add available audio layers
	for track := range w.AudioTracks {
		audioLayers = append(audioLayers, simulcastLayerResponse{
			EncodingID: w.AudioTracks[track].Rid,
		})
	}

	w.TracksLock.RUnlock()

	resp := map[string]map[string][]simulcastLayerResponse{
		"1": {
			"layers": videoLayers,
		},
		"2": {
			"layers": audioLayers,
		},
	}

	jsonResult, err := json.Marshal(resp)
	if err != nil {
		slog.Error("Error converting response to Json", "resp", resp, "err", err)
	}

	return "event: layers\ndata: " + string(jsonResult) + "\n\n"
}
