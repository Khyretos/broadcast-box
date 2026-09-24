package whip

import (
	"sync"
	"sync/atomic"

	"github.com/glimesh/broadcast-box/internal/clips"
	"github.com/glimesh/broadcast-box/internal/webrtc/codecs"
	"github.com/pion/webrtc/v4"
)

type (
	WHIPSession struct {
		ID             string
		PeerConnection *webrtc.PeerConnection
		closeOnce      sync.Once
		onClosed       func()
		onConnected    func()

		// Unix nanoseconds of the last received media packet, or of creation
		lastActivity atomic.Int64

		// Unix nanoseconds of the last keyframe request sent to the publisher
		lastPLI atomic.Int64

		PeerConnectionLock sync.RWMutex

		// Protects AudioTrack, VideoTracks
		TracksLock  sync.RWMutex
		VideoTracks map[string]*VideoTrack
		AudioTracks map[string]*AudioTrack

		// Clip buffer of the stream, nil when clips are disabled
		Recorder *clips.Recorder

		// TODO: WHEPSessionsSnapshot should contain serializable state, not runtime references.
		WHEPSessionsSnapshot atomic.Value
	}

	VideoTrack struct {
		Rid      string
		Priority int
		Bitrate  atomic.Uint64 // bytes per second

		// Detected from the stream, shown as the layer's quality
		Width           atomic.Uint32
		Height          atomic.Uint32
		FramesPerSecond atomic.Uint32 // hundredths of a frame per second

		PacketsReceived atomic.Uint64
		PacketsDropped  atomic.Uint64
		LastReceived    atomic.Value
		LastKeyFrame    atomic.Value
		MediaSSRC       atomic.Uint32
		Track           *codecs.TrackMultiCodec
	}
	AudioTrack struct {
		Rid             string
		Priority        int
		PacketsReceived atomic.Uint64
		PacketsDropped  atomic.Uint64
		LastReceived    atomic.Value
		Track           *codecs.TrackMultiCodec
	}
)
