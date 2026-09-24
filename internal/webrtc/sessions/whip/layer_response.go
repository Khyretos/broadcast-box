package whip

type (
	simulcastLayerResponse struct {
		EncodingID string `json:"encodingId"`

		// Video only, zero until detected
		Width           uint32  `json:"width,omitempty"`
		Height          uint32  `json:"height,omitempty"`
		FramesPerSecond float64 `json:"framesPerSecond,omitempty"`
		Bitrate         uint64  `json:"bitrate,omitempty"` // bits per second

		priority int
	}
)
