package clips

import (
	"bufio"
	"encoding/binary"
	"io"
	"math"
	"time"
)

// A minimal Matroska muxer. The whole file is planned first so element sizes,
// the seek index and cues are known up front; frame data is then streamed
// out cluster by cluster without holding the clip in memory.

const (
	ebmlIDHeader             = 0x1A45DFA3
	ebmlIDVersion            = 0x4286
	ebmlIDReadVersion        = 0x42F7
	ebmlIDMaxIDLength        = 0x42F2
	ebmlIDMaxSizeLength      = 0x42F3
	ebmlIDDocType            = 0x4282
	ebmlIDDocTypeVersion     = 0x4287
	ebmlIDDocTypeReadVersion = 0x4285

	mkvIDSegment          = 0x18538067
	mkvIDSeekHead         = 0x114D9B74
	mkvIDSeek             = 0x4DBB
	mkvIDSeekID           = 0x53AB
	mkvIDSeekPosition     = 0x53AC
	mkvIDInfo             = 0x1549A966
	mkvIDTimestampScale   = 0x2AD7B1
	mkvIDDuration         = 0x4489
	mkvIDDateUTC          = 0x4461
	mkvIDTitle            = 0x7BA9
	mkvIDMuxingApp        = 0x4D80
	mkvIDWritingApp       = 0x5741
	mkvIDTracks           = 0x1654AE6B
	mkvIDTrackEntry       = 0xAE
	mkvIDTrackNumber      = 0xD7
	mkvIDTrackUID         = 0x73C5
	mkvIDTrackType        = 0x83
	mkvIDFlagLacing       = 0x9C
	mkvIDCodecID          = 0x86
	mkvIDCodecPrivate     = 0x63A2
	mkvIDSeekPreRoll      = 0x56BB
	mkvIDVideo            = 0xE0
	mkvIDPixelWidth       = 0xB0
	mkvIDPixelHeight      = 0xBA
	mkvIDAudio            = 0xE1
	mkvIDSamplingFreq     = 0xB5
	mkvIDChannels         = 0x9F
	mkvIDCluster          = 0x1F43B675
	mkvIDClusterTimestamp = 0xE7
	mkvIDSimpleBlock      = 0xA3
	mkvIDCues             = 0x1C53BB6B
	mkvIDCuePoint         = 0xBB
	mkvIDCueTime          = 0xB3
	mkvIDCueTrackPosition = 0xB7
	mkvIDCueTrack         = 0xF7
	mkvIDCueClusterPos    = 0xF1

	mkvTrackTypeVideo = 1
	mkvTrackTypeAudio = 2

	mkvVideoTrackNumber = 1
	mkvAudioTrackNumber = 2

	// Relative block timestamps are int16 milliseconds
	maxClusterDuration = 5000

	// Size field length used for elements whose size is only known late
	largeSizeLength = 8
)

var matroskaEpoch = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

// A frame to be muxed. Data is loaded lazily so clips can be written from a
// file on disk as well as from memory.
type MuxFrame struct {
	Video    bool
	Keyframe bool
	PTS      time.Duration
	Size     int
	Read     func() ([]byte, error)
}

type MuxTracks struct {
	VideoCodecID  string // empty for audio only
	VideoPrivate  []byte
	Width, Height uint
	HasAudio      bool
}

type muxBlock struct {
	frame     *MuxFrame
	timestamp int64 // milliseconds
}

type muxCluster struct {
	timestamp   int64
	blocks      []muxBlock
	payloadSize int64
	position    int64 // relative to the segment data start
	cue         bool
}

type muxer struct {
	header   []byte // EBML header, segment start, seek head, info, tracks and cues
	clusters []*muxCluster
}

// Plans a Matroska file for the frames, which must be in decode order per
// track. The first video frame must be a keyframe.
func newMuxer(title string, createdAt time.Time, tracks MuxTracks, frames []MuxFrame) *muxer {
	blocks := interleave(frames)

	var clusters []*muxCluster
	var current *muxCluster
	for _, block := range blocks {
		startCluster := current == nil ||
			(block.frame.Video && block.frame.Keyframe && len(current.blocks) > 0) ||
			block.timestamp-current.timestamp > maxClusterDuration ||
			block.timestamp < current.timestamp-maxClusterDuration

		if startCluster {
			current = &muxCluster{timestamp: block.timestamp}
			current.cue = block.frame.Video && block.frame.Keyframe || tracks.VideoCodecID == ""
			current.payloadSize = int64(len(ebmlUint(mkvIDClusterTimestamp, uint64(block.timestamp))))
			clusters = append(clusters, current)
		}

		current.blocks = append(current.blocks, block)
		current.payloadSize += elementSize(mkvIDSimpleBlock, int64(simpleBlockHeaderSize+block.frame.Size))
	}

	var durationMs int64
	if len(blocks) > 0 {
		durationMs = blocks[len(blocks)-1].timestamp + estimateFrameDuration(blocks)
	}

	info := ebmlElement(mkvIDInfo,
		ebmlUint(mkvIDTimestampScale, uint64(time.Millisecond)),
		ebmlFloat(mkvIDDuration, float64(durationMs)),
		ebmlInt(mkvIDDateUTC, createdAt.Sub(matroskaEpoch).Nanoseconds()),
		ebmlString(mkvIDTitle, title),
		ebmlString(mkvIDMuxingApp, "Broadcast Box"),
		ebmlString(mkvIDWritingApp, "Broadcast Box"),
	)
	trackEntries := buildTracks(tracks)

	// Cue sizes don't depend on the cluster positions, which use a fixed
	// width, so the layout can be computed in one go
	cues := buildCues(clusters, tracks)
	seekHead := buildSeekHead(0, 0, 0)

	infoPosition := int64(len(seekHead))
	tracksPosition := infoPosition + int64(len(info))
	cuesPosition := tracksPosition + int64(len(trackEntries))
	position := cuesPosition + int64(len(cues))
	for _, cluster := range clusters {
		cluster.position = position
		position += int64(len(ebmlID(mkvIDCluster))) + largeSizeLength + cluster.payloadSize
	}

	seekHead = buildSeekHead(infoPosition, tracksPosition, cuesPosition)
	cues = buildCues(clusters, tracks)

	header := ebmlElement(ebmlIDHeader,
		ebmlUint(ebmlIDVersion, 1),
		ebmlUint(ebmlIDReadVersion, 1),
		ebmlUint(ebmlIDMaxIDLength, 4),
		ebmlUint(ebmlIDMaxSizeLength, 8),
		ebmlString(ebmlIDDocType, "matroska"),
		ebmlUint(ebmlIDDocTypeVersion, 4),
		ebmlUint(ebmlIDDocTypeReadVersion, 2),
	)
	header = append(header, ebmlID(mkvIDSegment)...)
	header = append(header, ebmlLargeSize(position)...)
	header = append(header, seekHead...)
	header = append(header, info...)
	header = append(header, trackEntries...)
	header = append(header, cues...)

	return &muxer{header: header, clusters: clusters}
}

// Total file size in bytes
func (m *muxer) Size() int64 {
	return int64(len(m.header)) + m.clustersSize()
}

func (m *muxer) clustersSize() int64 {
	var size int64
	for _, cluster := range m.clusters {
		size += int64(len(ebmlID(mkvIDCluster))) + largeSizeLength + cluster.payloadSize
	}
	return size
}

// WriteTo writes the file. onFrame, if set, receives the file offset of each
// frame's data in the order frames were written.
func (m *muxer) WriteTo(w io.Writer, onFrame func(frame *MuxFrame, offset int64)) (int64, error) {
	buffered := bufio.NewWriterSize(w, 256*1024)
	written := int64(0)
	write := func(data []byte) error {
		n, err := buffered.Write(data)
		written += int64(n)
		return err
	}

	if err := write(m.header); err != nil {
		return written, err
	}

	for _, cluster := range m.clusters {
		if err := write(ebmlID(mkvIDCluster)); err != nil {
			return written, err
		}
		if err := write(ebmlLargeSize(cluster.payloadSize)); err != nil {
			return written, err
		}
		if err := write(ebmlUint(mkvIDClusterTimestamp, uint64(cluster.timestamp))); err != nil {
			return written, err
		}

		for _, block := range cluster.blocks {
			data, err := block.frame.Read()
			if err != nil {
				return written, err
			}
			if len(data) != block.frame.Size {
				return written, io.ErrUnexpectedEOF
			}

			trackNumber := byte(0x80 | mkvVideoTrackNumber)
			if !block.frame.Video {
				trackNumber = byte(0x80 | mkvAudioTrackNumber)
			}
			flags := byte(0)
			if block.frame.Keyframe {
				flags = 0x80
			}

			blockHeader := append(ebmlID(mkvIDSimpleBlock), ebmlSize(int64(simpleBlockHeaderSize+len(data)))...)
			blockHeader = append(blockHeader, trackNumber)
			blockHeader = binary.BigEndian.AppendUint16(blockHeader, uint16(int16(block.timestamp-cluster.timestamp)))
			blockHeader = append(blockHeader, flags)
			if err := write(blockHeader); err != nil {
				return written, err
			}

			if onFrame != nil {
				onFrame(block.frame, written)
			}
			if err := write(data); err != nil {
				return written, err
			}
		}
	}

	return written, buffered.Flush()
}

// Track number, relative timestamp and flags
const simpleBlockHeaderSize = 4

// Merges video and audio into one timeline by timestamp, keeping each
// track's own order (video may contain frames out of presentation order)
func interleave(frames []MuxFrame) []muxBlock {
	var video, audio []*MuxFrame
	for i := range frames {
		if frames[i].Video {
			video = append(video, &frames[i])
		} else {
			audio = append(audio, &frames[i])
		}
	}

	var start time.Duration
	switch {
	case len(video) > 0:
		start = video[0].PTS
	case len(audio) > 0:
		start = audio[0].PTS
	}

	// Audio from before the first video keyframe can't be played with picture
	for len(audio) > 0 && len(video) > 0 && audio[0].PTS < start {
		audio = audio[1:]
	}

	blocks := make([]muxBlock, 0, len(video)+len(audio))
	for len(video) > 0 || len(audio) > 0 {
		var next *MuxFrame
		if len(audio) == 0 || (len(video) > 0 && video[0].PTS <= audio[0].PTS) {
			next, video = video[0], video[1:]
		} else {
			next, audio = audio[0], audio[1:]
		}
		blocks = append(blocks, muxBlock{frame: next, timestamp: int64((next.PTS - start) / time.Millisecond)})
	}
	return blocks
}

func estimateFrameDuration(blocks []muxBlock) int64 {
	var previous int64 = -1
	for i := len(blocks) - 1; i >= 0; i-- {
		if !blocks[i].frame.Video {
			continue
		}
		if previous >= 0 && previous > blocks[i].timestamp {
			return previous - blocks[i].timestamp
		}
		previous = blocks[i].timestamp
	}
	return 20
}

func buildTracks(tracks MuxTracks) []byte {
	var entries [][]byte

	if tracks.VideoCodecID != "" {
		video := [][]byte{
			ebmlUint(mkvIDTrackNumber, mkvVideoTrackNumber),
			ebmlUint(mkvIDTrackUID, mkvVideoTrackNumber),
			ebmlUint(mkvIDTrackType, mkvTrackTypeVideo),
			ebmlUint(mkvIDFlagLacing, 0),
			ebmlString(mkvIDCodecID, tracks.VideoCodecID),
		}
		if len(tracks.VideoPrivate) > 0 {
			video = append(video, ebmlElement(mkvIDCodecPrivate, tracks.VideoPrivate))
		}
		video = append(video, ebmlElement(mkvIDVideo,
			ebmlUint(mkvIDPixelWidth, uint64(tracks.Width)),
			ebmlUint(mkvIDPixelHeight, uint64(tracks.Height)),
		))
		entries = append(entries, ebmlElement(mkvIDTrackEntry, video...))
	}

	if tracks.HasAudio {
		entries = append(entries, ebmlElement(mkvIDTrackEntry,
			ebmlUint(mkvIDTrackNumber, mkvAudioTrackNumber),
			ebmlUint(mkvIDTrackUID, mkvAudioTrackNumber),
			ebmlUint(mkvIDTrackType, mkvTrackTypeAudio),
			ebmlUint(mkvIDFlagLacing, 0),
			ebmlString(mkvIDCodecID, "A_OPUS"),
			ebmlElement(mkvIDCodecPrivate, opusHead(2)),
			ebmlUint(mkvIDSeekPreRoll, uint64(80*time.Millisecond)),
			ebmlElement(mkvIDAudio,
				ebmlFloat(mkvIDSamplingFreq, 48000),
				ebmlUint(mkvIDChannels, 2),
			),
		))
	}

	return ebmlElement(mkvIDTracks, entries...)
}

func buildCues(clusters []*muxCluster, tracks MuxTracks) []byte {
	cueTrack := uint64(mkvVideoTrackNumber)
	if tracks.VideoCodecID == "" {
		cueTrack = mkvAudioTrackNumber
	}

	var points [][]byte
	for _, cluster := range clusters {
		if !cluster.cue {
			continue
		}
		points = append(points, ebmlElement(mkvIDCuePoint,
			ebmlUint(mkvIDCueTime, uint64(cluster.timestamp)),
			ebmlElement(mkvIDCueTrackPosition,
				ebmlUint(mkvIDCueTrack, cueTrack),
				ebmlFixedUint(mkvIDCueClusterPos, uint64(cluster.position)),
			),
		))
	}
	return ebmlElement(mkvIDCues, points...)
}

func buildSeekHead(info, tracks, cues int64) []byte {
	seek := func(id uint32, position int64) []byte {
		return ebmlElement(mkvIDSeek,
			ebmlElement(mkvIDSeekID, ebmlID(id)),
			ebmlFixedUint(mkvIDSeekPosition, uint64(position)),
		)
	}
	return ebmlElement(mkvIDSeekHead, seek(mkvIDInfo, info), seek(mkvIDTracks, tracks), seek(mkvIDCues, cues))
}

// EBML encoding

func ebmlID(id uint32) []byte {
	switch {
	case id >= 1<<24:
		return []byte{byte(id >> 24), byte(id >> 16), byte(id >> 8), byte(id)}
	case id >= 1<<16:
		return []byte{byte(id >> 16), byte(id >> 8), byte(id)}
	case id >= 1<<8:
		return []byte{byte(id >> 8), byte(id)}
	default:
		return []byte{byte(id)}
	}
}

// Variable length size using the fewest bytes
func ebmlSize(size int64) []byte {
	length := 1
	for length < 8 && uint64(size) >= (1<<(7*length))-1 {
		length++
	}
	out := make([]byte, length)
	value := uint64(size) | 1<<(7*length)
	for i := length - 1; i >= 0; i-- {
		out[i] = byte(value)
		value >>= 8
	}
	return out
}

// 8 byte size, used where the size is patched in or must have a fixed width
func ebmlLargeSize(size int64) []byte {
	out := make([]byte, 8)
	binary.BigEndian.PutUint64(out, uint64(size))
	out[0] = 0x01
	return out
}

func elementSize(id uint32, payloadSize int64) int64 {
	return int64(len(ebmlID(id))+len(ebmlSize(payloadSize))) + payloadSize
}

func ebmlElement(id uint32, children ...[]byte) []byte {
	size := 0
	for _, child := range children {
		size += len(child)
	}
	out := append(ebmlID(id), ebmlSize(int64(size))...)
	for _, child := range children {
		out = append(out, child...)
	}
	return out
}

func ebmlUint(id uint32, value uint64) []byte {
	length := 1
	for length < 8 && value >= 1<<(8*length) {
		length++
	}
	payload := make([]byte, length)
	for i := length - 1; i >= 0; i-- {
		payload[i] = byte(value)
		value >>= 8
	}
	return ebmlElement(id, payload)
}

func ebmlFixedUint(id uint32, value uint64) []byte {
	return ebmlElement(id, binary.BigEndian.AppendUint64(nil, value))
}

func ebmlInt(id uint32, value int64) []byte {
	return ebmlElement(id, binary.BigEndian.AppendUint64(nil, uint64(value)))
}

func ebmlFloat(id uint32, value float64) []byte {
	return ebmlElement(id, binary.BigEndian.AppendUint64(nil, math.Float64bits(value)))
}

func ebmlString(id uint32, value string) []byte {
	return ebmlElement(id, []byte(value))
}
