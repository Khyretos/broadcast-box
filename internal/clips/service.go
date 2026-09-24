package clips

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/glimesh/broadcast-box/internal/webrtc/codecs"
	"golang.org/x/time/rate"
)

const (
	defaultBufferDuration = 2 * time.Minute
	defaultMaxDrafts      = 10
	draftTTL              = 10 * time.Minute
	minClipDuration       = time.Second
	maxTitleLength        = 100
	maxListedClips        = 200
	listCacheDuration     = 30 * time.Second
)

var (
	ErrDisabled      = errors.New("clips are not enabled on this server")
	ErrDraftNotFound = errors.New("clip draft not found or expired")
	ErrInvalidRange  = errors.New("invalid clip range")
	ErrInvalidKey    = errors.New("stream key can not be used for clips")
	ErrRateLimited   = errors.New("too many clips, try again in a few seconds")
	validStreamKey   = regexp.MustCompile(`^[\p{L}\p{N}_-][\p{L}\p{N}_.-]*$`)
	validClipID      = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[0-9a-f]{8}$`)
)

type Config struct {
	BufferDuration  time.Duration
	MaxClipDuration time.Duration
	DraftDirectory  string
	MaxDrafts       int
}

// Clip metadata, stored as <id>.json next to <id>.mkv
type Clip struct {
	ID              string    `json:"id"`
	StreamKey       string    `json:"streamKey"`
	Title           string    `json:"title"`
	CreatedAt       time.Time `json:"createdAt"`
	DurationSeconds float64   `json:"durationSeconds"`
	SizeBytes       int64     `json:"sizeBytes"`
	VideoCodec      string    `json:"videoCodec,omitempty"`
	Width           uint      `json:"width,omitempty"`
	Height          uint      `json:"height,omitempty"`
}

type Draft struct {
	ID              string  `json:"id"`
	StreamKey       string  `json:"streamKey"`
	DurationSeconds float64 `json:"durationSeconds"`
	MaxClipSeconds  float64 `json:"maxClipSeconds"`

	createdAt time.Time
	path      string
	tracks    MuxTracks
	codec     string
	frames    []draftFrame
}

type draftFrame struct {
	video, keyframe bool
	pts             time.Duration // relative to the draft start
	offset          int64
	size            int
}

type cachedList struct {
	clips   []Clip
	fetched time.Time
}

type Service struct {
	storage Storage
	config  Config

	lock      sync.Mutex
	drafts    map[string]*Draft
	listCache map[string]cachedList
	limiters  map[string]*clientLimiter
}

type clientLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func NewService(storage Storage, config Config) (*Service, error) {
	if config.BufferDuration <= 0 {
		config.BufferDuration = defaultBufferDuration
	}
	if config.MaxClipDuration <= 0 || config.MaxClipDuration > config.BufferDuration {
		config.MaxClipDuration = config.BufferDuration
	}
	if config.MaxDrafts <= 0 {
		config.MaxDrafts = defaultMaxDrafts
	}

	// Drafts don't survive restarts
	_ = os.RemoveAll(config.DraftDirectory)
	if err := os.MkdirAll(config.DraftDirectory, 0o755); err != nil {
		return nil, fmt.Errorf("creating clip draft directory: %w", err)
	}

	s := &Service{
		storage:   storage,
		config:    config,
		drafts:    map[string]*Draft{},
		listCache: map[string]cachedList{},
		limiters:  map[string]*clientLimiter{},
	}
	go s.cleanupLoop()
	return s, nil
}

func (s *Service) Config() Config { return s.config }

// Allow is a per client rate limit for creating drafts and publishing clips
func (s *Service) Allow(client string) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	entry, ok := s.limiters[client]
	if !ok {
		entry = &clientLimiter{limiter: rate.NewLimiter(rate.Every(10*time.Second), 3)}
		s.limiters[client] = entry
	}
	entry.lastSeen = time.Now()
	return entry.limiter.Allow()
}

func ValidStreamKey(streamKey string) bool {
	return len(streamKey) <= 128 && validStreamKey.MatchString(streamKey)
}

// CreateDraft snapshots the recorder into a temporary Matroska file that can
// be previewed and trimmed before publishing
func (s *Service) CreateDraft(streamKey string, recorder *Recorder) (*Draft, error) {
	if !ValidStreamKey(streamKey) {
		return nil, ErrInvalidKey
	}

	snapshot, err := recorder.Snapshot()
	if err != nil {
		return nil, err
	}

	tracks, codecName, err := tracksForSnapshot(snapshot)
	if err != nil {
		return nil, err
	}

	muxFrames := make([]MuxFrame, len(snapshot.Frames))
	for i, frame := range snapshot.Frames {
		muxFrames[i] = MuxFrame{
			Video:    frame.Video,
			Keyframe: frame.Keyframe,
			PTS:      frame.PTS,
			Size:     len(frame.Data),
			Read:     func() ([]byte, error) { return frame.Data, nil },
		}
	}

	draft := &Draft{
		ID:             newID(snapshot.CreatedAt),
		StreamKey:      streamKey,
		MaxClipSeconds: s.config.MaxClipDuration.Seconds(),
		createdAt:      snapshot.CreatedAt,
		tracks:         tracks,
		codec:          codecName,
	}
	draft.path = filepath.Join(s.config.DraftDirectory, draft.ID+".mkv")

	file, err := os.Create(draft.path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var start time.Duration
	started := false
	muxer := newMuxer(streamKey, snapshot.CreatedAt, tracks, muxFrames)
	if _, err := muxer.WriteTo(file, func(frame *MuxFrame, offset int64) {
		if !started {
			start, started = frame.PTS, true
		}
		draft.frames = append(draft.frames, draftFrame{
			video:    frame.Video,
			keyframe: frame.Keyframe,
			pts:      frame.PTS - start,
			offset:   offset,
			size:     frame.Size,
		})
	}); err != nil {
		os.Remove(draft.path)
		return nil, err
	}

	if len(draft.frames) > 0 {
		draft.DurationSeconds = draft.frames[len(draft.frames)-1].pts.Seconds()
	}

	s.lock.Lock()
	for len(s.drafts) >= s.config.MaxDrafts {
		s.removeOldestDraftLocked()
	}
	s.drafts[draft.ID] = draft
	s.lock.Unlock()

	slog.Info("Clips: draft created", "streamKey", streamKey, "id", draft.ID, "duration", draft.DurationSeconds)
	return draft, nil
}

func (s *Service) draft(id string) (*Draft, error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	draft, ok := s.drafts[id]
	if !ok {
		return nil, ErrDraftNotFound
	}
	return draft, nil
}

func (s *Service) ServeDraft(w http.ResponseWriter, r *http.Request, id string) {
	draft, err := s.draft(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	file, err := os.Open(draft.path)
	if err != nil {
		http.Error(w, ErrDraftNotFound.Error(), http.StatusNotFound)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", "video/x-matroska")
	w.Header().Set("Cache-Control", "private, max-age=600")
	http.ServeContent(w, r, "", draft.createdAt, file)
}

// Publish cuts [start, end] (seconds from the start of the draft) out of the
// draft and stores it. The clip starts at the keyframe at or before start.
func (s *Service) Publish(ctx context.Context, id string, start, end float64, title string) (*Clip, error) {
	draft, err := s.draft(id)
	if err != nil {
		return nil, err
	}

	startPTS := time.Duration(start * float64(time.Second))
	endPTS := time.Duration(end * float64(time.Second))
	if start < 0 || endPTS-startPTS < minClipDuration || endPTS-startPTS > s.config.MaxClipDuration+time.Second ||
		start > draft.DurationSeconds {
		return nil, ErrInvalidRange
	}

	frames := selectFrames(draft.frames, draft.tracks.VideoCodecID != "", startPTS, endPTS)
	if len(frames) == 0 {
		return nil, ErrInvalidRange
	}

	file, err := os.Open(draft.path)
	if err != nil {
		return nil, ErrDraftNotFound
	}
	defer file.Close()

	muxFrames := make([]MuxFrame, len(frames))
	for i, frame := range frames {
		muxFrames[i] = MuxFrame{
			Video:    frame.video,
			Keyframe: frame.keyframe,
			PTS:      frame.pts,
			Size:     frame.size,
			Read: func() ([]byte, error) {
				data := make([]byte, frame.size)
				_, err := file.ReadAt(data, frame.offset)
				return data, err
			},
		}
	}

	createdAt := time.Now().UTC()
	clip := &Clip{
		ID:              newID(createdAt),
		StreamKey:       draft.StreamKey,
		Title:           sanitizeTitle(title),
		CreatedAt:       createdAt,
		DurationSeconds: (frames[len(frames)-1].pts - frames[0].pts).Seconds(),
		VideoCodec:      draft.codec,
		Width:           draft.tracks.Width,
		Height:          draft.tracks.Height,
	}
	if clip.Title == "" {
		clip.Title = "Clip " + createdAt.Format("2006-01-02 15:04:05") + " UTC"
	}

	muxer := newMuxer(clip.Title, createdAt, draft.tracks, muxFrames)
	clip.SizeBytes = muxer.Size()

	reader, writer := io.Pipe()
	go func() {
		_, err := muxer.WriteTo(writer, nil)
		writer.CloseWithError(err)
	}()

	if err := s.storage.Save(ctx, clipKey(clip.StreamKey, clip.ID, ".mkv"), reader, clip.SizeBytes, "video/x-matroska"); err != nil {
		reader.CloseWithError(err)
		return nil, fmt.Errorf("saving clip: %w", err)
	}

	metadata, _ := json.MarshalIndent(clip, "", "  ")
	if err := s.storage.Save(ctx, clipKey(clip.StreamKey, clip.ID, ".json"), strings.NewReader(string(metadata)), int64(len(metadata)), "application/json"); err != nil {
		return nil, fmt.Errorf("saving clip metadata: %w", err)
	}

	s.lock.Lock()
	delete(s.listCache, clip.StreamKey)
	s.lock.Unlock()

	slog.Info("Clips: clip published", "streamKey", clip.StreamKey, "id", clip.ID, "title", clip.Title, "duration", clip.DurationSeconds, "storage", s.storage.Name())
	return clip, nil
}

// Picks the frames for [start, end], starting at the keyframe at or before start
func selectFrames(frames []draftFrame, hasVideo bool, start, end time.Duration) []draftFrame {
	keyframe := -1
	for i, frame := range frames {
		if !hasVideo || (frame.video && frame.keyframe) {
			if frame.pts <= start || keyframe < 0 {
				keyframe = i
			}
			if frame.pts > start {
				break
			}
		}
	}
	if keyframe < 0 {
		return nil
	}

	clipStart := frames[keyframe].pts
	var selected []draftFrame
	for _, frame := range frames[keyframe:] {
		if frame.pts > end {
			continue
		}
		if !frame.video && frame.pts < clipStart {
			continue
		}
		selected = append(selected, frame)
	}
	return selected
}

func (s *Service) List(ctx context.Context, streamKey string) ([]Clip, error) {
	if !ValidStreamKey(streamKey) {
		return nil, ErrInvalidKey
	}

	s.lock.Lock()
	cached, ok := s.listCache[streamKey]
	s.lock.Unlock()
	if ok && time.Since(cached.fetched) < listCacheDuration {
		return cached.clips, nil
	}

	keys, err := s.storage.List(ctx, streamKey)
	if err != nil {
		return nil, err
	}

	var metadataKeys []string
	for _, key := range keys {
		if path.Ext(key) == ".json" {
			metadataKeys = append(metadataKeys, key)
		}
	}
	// IDs start with the creation time, newest first
	slices.Sort(metadataKeys)
	slices.Reverse(metadataKeys)
	if len(metadataKeys) > maxListedClips {
		metadataKeys = metadataKeys[:maxListedClips]
	}

	clips := make([]Clip, 0, len(metadataKeys))
	for _, key := range metadataKeys {
		data, err := s.storage.Read(ctx, key)
		if err != nil {
			continue
		}
		var clip Clip
		if json.Unmarshal(data, &clip) == nil && validClipID.MatchString(clip.ID) {
			clip.StreamKey = streamKey
			clips = append(clips, clip)
		}
	}

	s.lock.Lock()
	s.listCache[streamKey] = cachedList{clips: clips, fetched: time.Now()}
	s.lock.Unlock()

	return clips, nil
}

func (s *Service) Serve(w http.ResponseWriter, r *http.Request, streamKey, id string, download bool) {
	if !ValidStreamKey(streamKey) || !validClipID.MatchString(id) {
		http.Error(w, ErrNotFound.Error(), http.StatusNotFound)
		return
	}

	downloadName := ""
	if download {
		downloadName = streamKey + "-" + id + ".mkv"
	}
	s.storage.Serve(w, r, clipKey(streamKey, id, ".mkv"), downloadName)
}

func (s *Service) Delete(ctx context.Context, streamKey, id string) error {
	if !ValidStreamKey(streamKey) || !validClipID.MatchString(id) {
		return ErrNotFound
	}

	err := s.storage.Delete(ctx, clipKey(streamKey, id, ".mkv"))
	if metadataErr := s.storage.Delete(ctx, clipKey(streamKey, id, ".json")); err == nil {
		err = metadataErr
	}

	s.lock.Lock()
	delete(s.listCache, streamKey)
	s.lock.Unlock()

	return err
}

func (s *Service) cleanupLoop() {
	for range time.Tick(30 * time.Second) {
		s.lock.Lock()
		for id, draft := range s.drafts {
			if time.Since(draft.createdAt) > draftTTL {
				os.Remove(draft.path)
				delete(s.drafts, id)
			}
		}
		for client, entry := range s.limiters {
			if time.Since(entry.lastSeen) > 10*time.Minute {
				delete(s.limiters, client)
			}
		}
		s.lock.Unlock()
	}
}

func (s *Service) removeOldestDraftLocked() {
	var oldest *Draft
	for _, draft := range s.drafts {
		if oldest == nil || draft.createdAt.Before(oldest.createdAt) {
			oldest = draft
		}
	}
	if oldest != nil {
		os.Remove(oldest.path)
		delete(s.drafts, oldest.ID)
	}
}

// Finds the codec configuration and picture size for the snapshot's video
func tracksForSnapshot(snapshot *Snapshot) (tracks MuxTracks, codecName string, err error) {
	var keyframe *Frame
	for _, frame := range snapshot.Frames {
		if frame.Video && frame.Keyframe && keyframe == nil {
			keyframe = frame
		}
		if !frame.Video {
			tracks.HasAudio = true
		}
	}

	if snapshot.VideoCodec == 0 || keyframe == nil {
		return tracks, "", nil
	}

	switch snapshot.VideoCodec {
	case codecs.VideoTrackCodecH264:
		sps, pps := snapshot.H264SPS, snapshot.H264PPS
		if sps == nil || pps == nil {
			return tracks, "", errors.New("no H264 parameter sets received yet")
		}
		info, err := parseH264SPS(sps)
		if err != nil {
			return tracks, "", fmt.Errorf("parsing H264 SPS: %w", err)
		}
		tracks.VideoCodecID, codecName = "V_MPEG4/ISO/AVC", "h264"
		tracks.VideoPrivate = h264AVCC(sps, pps, info)
		tracks.Width, tracks.Height = info.width, info.height

	case codecs.VideoTrackCodecAV1:
		if snapshot.AV1SequenceHeader == nil {
			return tracks, "", errors.New("no AV1 sequence header received yet")
		}
		sequenceHeader := snapshot.AV1SequenceHeader
		obus, _ := splitAV1OBUs(sequenceHeader)
		if len(obus) == 0 {
			return tracks, "", errors.New("invalid AV1 sequence header")
		}
		info, err := parseAV1SequenceHeader(obus[0].payload)
		if err != nil {
			return tracks, "", fmt.Errorf("parsing AV1 sequence header: %w", err)
		}
		tracks.VideoCodecID, codecName = "V_AV1", "av1"
		tracks.VideoPrivate = av1C(sequenceHeader, info)
		tracks.Width, tracks.Height = info.width, info.height

	case codecs.VideoTrackCodecVP8:
		tracks.VideoCodecID, codecName = "V_VP8", "vp8"
		tracks.Width, tracks.Height = vp8Dimensions(keyframe.Data)

	case codecs.VideoTrackCodecVP9:
		tracks.VideoCodecID, codecName = "V_VP9", "vp9"
		info := parseVP9Frame(keyframe.Data)
		tracks.Width, tracks.Height = info.width, info.height

	default:
		return tracks, "", ErrUnsupportedCodec
	}

	return tracks, codecName, nil
}

func clipKey(streamKey, id, extension string) string {
	return streamKey + "/" + id + extension
}

func newID(at time.Time) string {
	random := make([]byte, 4)
	_, _ = rand.Read(random)
	return at.UTC().Format("20060102-150405") + "-" + hex.EncodeToString(random)
}

func sanitizeTitle(title string) string {
	title = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(title))

	if runes := []rune(title); len(runes) > maxTitleLength {
		title = string(runes[:maxTitleLength])
	}
	return title
}
