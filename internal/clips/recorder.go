package clips

import (
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/glimesh/broadcast-box/internal/webrtc/codecs"
	"github.com/pion/rtp"
	pionCodecs "github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"
)

const (
	recorderQueueSize = 8192

	// Packets a sample may be late before the sample builder gives up on it
	samplebuilderMaxLate = 512

	// Timestamps further than this from the arrival time are treated as a
	// discontinuity (e.g. encoder restart) and re-anchored
	maxTimestampDrift = 3 * time.Second
)

var (
	ErrNothingRecorded  = errors.New("nothing has been recorded yet")
	ErrUnsupportedCodec = errors.New("clips are not supported for this video codec")
)

// A complete video or audio frame in the clip buffer. Frames are immutable
// once recorded and shared between the buffer and snapshots.
type Frame struct {
	Video    bool
	Keyframe bool
	PTS      time.Duration // relative to the recorder epoch
	Data     []byte        // Matroska ready: AVCC for H264, OBUs without temporal delimiters for AV1
}

type recorderInput struct {
	packet   *rtp.Packet
	video    bool
	rid      string
	priority int
	codec    codecs.TrackCodeType
	arrival  time.Time
}

type trackTimeline struct {
	builder   *samplebuilder.SampleBuilder
	codec     codecs.TrackCodeType
	clockRate uint32

	anchored   bool
	anchorRTP  int64
	anchorPTS  time.Duration
	lastRTP    uint32
	unwrapped  int64
	lastPushed time.Time
}

// Recorder keeps the last bufferDuration of a stream so viewers can clip it.
// Packets are pushed from the ingest loop and processed on a separate
// goroutine; if it falls behind, packets are dropped rather than slowing
// down the stream.
type Recorder struct {
	bufferDuration time.Duration
	input          chan recorderInput
	epoch          time.Time

	// Only accessed from the run goroutine
	video, audio  *trackTimeline
	videoRID      string
	videoPriority int

	lock       sync.Mutex
	frames     []*Frame
	videoCodec codecs.TrackCodeType

	// Latest codec configuration, in case the encoder doesn't repeat it
	h264SPS, h264PPS  []byte
	av1SequenceHeader []byte

	bytes     int
	closed    chan struct{}
	closeOnce sync.Once
}

func NewRecorder(bufferDuration time.Duration) *Recorder {
	r := &Recorder{
		bufferDuration: bufferDuration,
		input:          make(chan recorderInput, recorderQueueSize),
		epoch:          time.Now(),
		closed:         make(chan struct{}),
	}
	go r.run()
	return r
}

func (r *Recorder) Close() {
	r.closeOnce.Do(func() { close(r.closed) })
}

// PushVideo records a video packet. With simulcast only the best layer
// (lowest priority value) is recorded. The packet must not be modified after.
func (r *Recorder) PushVideo(packet *rtp.Packet, rid string, priority int, codec codecs.TrackCodeType) {
	r.push(recorderInput{packet: packet, video: true, rid: rid, priority: priority, codec: codec, arrival: time.Now()})
}

// PushAudio records an Opus packet. The packet must not be modified after.
func (r *Recorder) PushAudio(packet *rtp.Packet) {
	r.push(recorderInput{packet: packet, arrival: time.Now()})
}

func (r *Recorder) push(input recorderInput) {
	select {
	case r.input <- input:
	default:
		// Falling behind, the next keyframe resynchronises the buffer
	}
}

// Reset clears the buffer, e.g. when a new publisher connects
func (r *Recorder) Reset() {
	r.push(recorderInput{})
}

func (r *Recorder) run() {
	for {
		select {
		case <-r.closed:
			return
		case input := <-r.input:
			switch {
			case input.packet == nil:
				r.reset()
			case input.video:
				r.handleVideo(input)
			default:
				r.handleAudio(input)
			}
		}
	}
}

func (r *Recorder) reset() {
	r.video, r.audio = nil, nil
	r.videoRID, r.videoPriority = "", 0

	r.lock.Lock()
	r.frames, r.bytes, r.videoCodec = nil, 0, 0
	r.h264SPS, r.h264PPS, r.av1SequenceHeader = nil, nil, nil
	r.lock.Unlock()
}

func newVideoTimeline(codec codecs.TrackCodeType) *trackTimeline {
	var depacketizer rtp.Depacketizer
	switch codec {
	case codecs.VideoTrackCodecH264:
		depacketizer = &pionCodecs.H264Packet{}
	case codecs.VideoTrackCodecAV1:
		depacketizer = &pionCodecs.AV1Depacketizer{}
	case codecs.VideoTrackCodecVP8:
		depacketizer = &pionCodecs.VP8Packet{}
	case codecs.VideoTrackCodecVP9:
		depacketizer = &pionCodecs.VP9Packet{}
	default:
		return nil
	}

	return &trackTimeline{
		builder:   samplebuilder.New(samplebuilderMaxLate, depacketizer, 90000),
		codec:     codec,
		clockRate: 90000,
	}
}

func (r *Recorder) handleVideo(input recorderInput) {
	switch {
	case r.video == nil || input.codec != r.video.codec:
		// First packet, or the publisher changed codec
		if r.video != nil {
			r.reset()
		}
		r.video = newVideoTimeline(input.codec)
		r.videoRID, r.videoPriority = input.rid, input.priority

		r.lock.Lock()
		r.videoCodec = input.codec
		r.lock.Unlock()

		if r.video == nil {
			slog.Info("Clips: video codec not supported for clips", "codec", input.codec)
		}
	case input.rid != r.videoRID:
		if input.priority >= r.videoPriority {
			return
		}
		// A better simulcast layer showed up, record that one instead
		r.reset()
		r.video = newVideoTimeline(input.codec)
		r.videoRID, r.videoPriority = input.rid, input.priority
		r.lock.Lock()
		r.videoCodec = input.codec
		r.lock.Unlock()
	}

	if r.video == nil {
		return
	}

	r.video.builder.Push(input.packet)
	r.video.lastPushed = input.arrival
	for sample := r.video.builder.Pop(); sample != nil; sample = r.video.builder.Pop() {
		r.addVideoSample(sample.Data, sample.PacketTimestamp, input.arrival)
	}
}

func (r *Recorder) handleAudio(input recorderInput) {
	if r.audio == nil {
		r.audio = &trackTimeline{
			builder:   samplebuilder.New(samplebuilderMaxLate, &pionCodecs.OpusPacket{}, 48000),
			clockRate: 48000,
		}
	}

	r.audio.builder.Push(input.packet)
	for sample := r.audio.builder.Pop(); sample != nil; sample = r.audio.builder.Pop() {
		if len(sample.Data) == 0 {
			continue
		}
		r.addFrame(&Frame{
			Keyframe: true,
			PTS:      r.audio.pts(sample.PacketTimestamp, input.arrival.Sub(r.epoch)),
			Data:     sample.Data,
		})
	}
}

func (r *Recorder) addVideoSample(data []byte, rtpTimestamp uint32, arrival time.Time) {
	if len(data) == 0 {
		return
	}

	frame := &Frame{Video: true, PTS: r.video.pts(rtpTimestamp, arrival.Sub(r.epoch))}
	switch r.video.codec {
	case codecs.VideoTrackCodecH264:
		frame.Keyframe = h264IsKeyframe(data)
		frame.Data = h264AnnexBToAVCC(data)
		if sps, pps := h264ParameterSets(data); sps != nil || pps != nil {
			r.lock.Lock()
			if sps != nil {
				r.h264SPS = append([]byte(nil), sps...)
			}
			if pps != nil {
				r.h264PPS = append([]byte(nil), pps...)
			}
			r.lock.Unlock()
		}
	case codecs.VideoTrackCodecAV1:
		frame.Keyframe = av1IsKeyframe(data)
		frame.Data = av1StripOBUs(data)
		if sequenceHeader := av1SequenceHeader(data); sequenceHeader != nil {
			r.lock.Lock()
			r.av1SequenceHeader = append([]byte(nil), sequenceHeader.data...)
			r.lock.Unlock()
		}
	case codecs.VideoTrackCodecVP8:
		frame.Keyframe = vp8IsKeyframe(data)
		frame.Data = data
	case codecs.VideoTrackCodecVP9:
		frame.Keyframe = parseVP9Frame(data).keyframe
		frame.Data = data
	}

	r.addFrame(frame)
}

// Maps an RTP timestamp to the recorder timeline. The first packet anchors
// the RTP clock to its arrival time, after that the RTP clock is followed so
// frame timing stays smooth.
func (t *trackTimeline) pts(rtpTimestamp uint32, arrival time.Duration) time.Duration {
	if t.anchored {
		t.unwrapped += int64(int32(rtpTimestamp - t.lastRTP))
	} else {
		t.unwrapped = int64(rtpTimestamp)
	}
	t.lastRTP = rtpTimestamp

	pts := t.anchorPTS + time.Duration((t.unwrapped-t.anchorRTP)*int64(time.Second)/int64(t.clockRate))
	if !t.anchored || pts-arrival > maxTimestampDrift || arrival-pts > maxTimestampDrift {
		t.anchored = true
		t.anchorRTP = t.unwrapped
		t.anchorPTS = arrival
		pts = arrival
	}

	return pts
}

func (r *Recorder) addFrame(frame *Frame) {
	r.lock.Lock()
	defer r.lock.Unlock()

	if frame.Video && len(r.frames) == 0 && !frame.Keyframe {
		return // the buffer always starts at a keyframe
	}

	r.frames = append(r.frames, frame)
	r.bytes += len(frame.Data)
	r.trimLocked(frame.PTS)
}

// Drops frames older than the buffer duration, keeping the buffer starting
// at a video keyframe so every snapshot is decodable
func (r *Recorder) trimLocked(newest time.Duration) {
	cutoff := newest - r.bufferDuration
	if len(r.frames) == 0 || r.frames[0].PTS >= cutoff {
		return
	}

	start := -1
	for i, frame := range r.frames {
		if frame.PTS < cutoff {
			continue
		}
		if r.videoCodec == 0 || (frame.Video && frame.Keyframe) {
			start = i
			break
		}
	}
	if start <= 0 {
		return
	}

	for _, frame := range r.frames[:start] {
		r.bytes -= len(frame.Data)
	}
	remaining := r.frames[start:]

	// Let the dropped frames be garbage collected once the slice has shrunk
	if cap(r.frames) > 2*len(remaining)+1024 {
		remaining = append(make([]*Frame, 0, len(remaining)+1024), remaining...)
	}
	r.frames = remaining
}

// Snapshot of the buffer. Frames are shared and must not be modified.
type Snapshot struct {
	VideoCodec codecs.TrackCodeType
	Frames     []*Frame
	CreatedAt  time.Time

	H264SPS, H264PPS  []byte
	AV1SequenceHeader []byte
}

func (s *Snapshot) Duration() time.Duration {
	if len(s.Frames) < 2 {
		return 0
	}
	return s.Frames[len(s.Frames)-1].PTS - s.Frames[0].PTS
}

func (r *Recorder) Snapshot() (*Snapshot, error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	switch r.videoCodec {
	case 0, codecs.VideoTrackCodecH264, codecs.VideoTrackCodecAV1, codecs.VideoTrackCodecVP8, codecs.VideoTrackCodecVP9:
	default:
		return nil, ErrUnsupportedCodec
	}

	hasKeyframe := false
	for _, frame := range r.frames {
		if frame.Video && frame.Keyframe {
			hasKeyframe = true
			break
		}
	}
	if len(r.frames) == 0 || (r.videoCodec != 0 && !hasKeyframe) {
		return nil, ErrNothingRecorded
	}

	return &Snapshot{
		VideoCodec:        r.videoCodec,
		Frames:            append([]*Frame(nil), r.frames...),
		CreatedAt:         time.Now(),
		H264SPS:           r.h264SPS,
		H264PPS:           r.h264PPS,
		AV1SequenceHeader: r.av1SequenceHeader,
	}, nil
}

// Size of the buffered frames in bytes
func (r *Recorder) BufferedBytes() int {
	r.lock.Lock()
	defer r.lock.Unlock()
	return r.bytes
}
