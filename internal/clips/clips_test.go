package clips

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/glimesh/broadcast-box/internal/webrtc/codecs"
	"github.com/pion/rtp"
	pionCodecs "github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4/pkg/media/h264reader"
	"github.com/pion/webrtc/v4/pkg/media/ivfreader"
	"github.com/pion/webrtc/v4/pkg/media/oggreader"
	"github.com/stretchr/testify/require"
)

// Encodes test media with ffmpeg, streams it through the recorder as RTP and
// checks that ffmpeg decodes the published clip without errors.

const (
	testFPS      = 30
	testSeconds  = 12
	testKeyframe = 60 // every 2 seconds
)

func requireFFmpeg(t *testing.T) string {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	return ffmpeg
}

func ffmpegRun(t *testing.T, ffmpeg string, args ...string) {
	output, err := exec.Command(ffmpeg, append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)...).CombinedOutput()
	require.NoError(t, err, string(output))
}

func encodeTestMedia(t *testing.T, ffmpeg, videoArgs, videoFile string) (video, audio string) {
	dir := t.TempDir()
	video = filepath.Join(dir, videoFile)
	audio = filepath.Join(dir, "audio.ogg")

	source := "testsrc2=size=320x240:rate=" + strconv.Itoa(testFPS) + ":duration=" + strconv.Itoa(testSeconds)
	args := []string{"-f", "lavfi", "-i", source}
	args = append(args, splitArgs(videoArgs)...)
	ffmpegRun(t, ffmpeg, append(args, video)...)
	ffmpegRun(t, ffmpeg, "-f", "lavfi", "-i", "sine=frequency=440:duration="+strconv.Itoa(testSeconds),
		"-ac", "2", "-c:a", "libopus", "-b:a", "64k", "-frame_duration", "20", "-page_duration", "20000", audio)
	return video, audio
}

func splitArgs(args string) []string {
	return regexp.MustCompile(`\s+`).Split(args, -1)
}

// Feeds audio packets into the recorder, paced against the media timeline
func pushAudio(t *testing.T, r *Recorder, audioFile string, epoch time.Time) {
	file, err := os.Open(audioFile)
	require.NoError(t, err)
	defer file.Close()

	ogg, _, err := oggreader.NewWith(file)
	require.NoError(t, err)

	sequence := uint16(0)
	timestamp := uint32(0)
	for {
		pages, _, err := ogg.ParseNextPage()
		if err == io.EOF {
			return
		}
		require.NoError(t, err)
		if bytes.HasPrefix(pages, []byte("OpusHead")) || bytes.HasPrefix(pages, []byte("OpusTags")) {
			continue
		}

		r.push(recorderInput{
			packet:  &rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: sequence, Timestamp: 1000 + timestamp}, Payload: pages},
			arrival: epoch.Add(time.Duration(timestamp) * time.Second / 48000),
		})
		sequence++
		timestamp += 960 // 20ms
	}
}

func pushVideoFrames(r *Recorder, frames [][]byte, payloader rtp.Payloader, codec codecs.TrackCodeType, epoch time.Time) {
	sequence := uint16(0)
	for i, frame := range frames {
		timestamp := uint32(i * 90000 / testFPS)
		payloads := payloader.Payload(1200, frame)
		for j, payload := range payloads {
			r.push(recorderInput{
				packet: &rtp.Packet{
					Header:  rtp.Header{Version: 2, SequenceNumber: sequence, Timestamp: 5000 + timestamp, Marker: j == len(payloads)-1},
					Payload: payload,
				},
				video:   true,
				rid:     "0",
				codec:   codec,
				arrival: epoch.Add(time.Duration(i) * time.Second / testFPS),
			})
			sequence++
		}
	}
}

func waitForRecorder(t *testing.T, r *Recorder) {
	require.Eventually(t, func() bool { return len(r.input) == 0 }, 5*time.Second, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
}

// Decodes the whole file and returns the number of decoded video frames,
// the container duration in seconds and ffmpeg's description of the file
func decodeClip(t *testing.T, ffmpeg, file string) (int, float64, string) {
	output, err := exec.Command(ffmpeg, "-hide_banner", "-v", "error", "-stats", "-i", file, "-f", "null", "-").CombinedOutput()
	require.NoError(t, err, string(output))

	// Anything other than progress lines is a decoding error
	for _, line := range regexp.MustCompile(`\r|\n`).Split(string(output), -1) {
		if line != "" && !regexp.MustCompile(`^(frame=|size=)`).MatchString(strings.TrimSpace(line)) {
			t.Errorf("ffmpeg reported: %s", line)
		}
	}

	frameMatches := regexp.MustCompile(`frame=\s*(\d+)`).FindAllStringSubmatch(string(output), -1)
	require.NotEmpty(t, frameMatches, string(output))
	frames, _ := strconv.Atoi(frameMatches[len(frameMatches)-1][1])

	info, _ := exec.Command(ffmpeg, "-hide_banner", "-i", file).CombinedOutput()
	duration := regexp.MustCompile(`Duration: (\d+):(\d+):(\d+\.\d+)`).FindStringSubmatch(string(info))
	require.NotEmpty(t, duration, string(info))
	hours, _ := strconv.ParseFloat(duration[1], 64)
	minutes, _ := strconv.ParseFloat(duration[2], 64)
	seconds, _ := strconv.ParseFloat(duration[3], 64)

	return frames, hours*3600 + minutes*60 + seconds, string(info)
}

func runClipTest(t *testing.T, ffmpeg string, codec codecs.TrackCodeType, frames [][]byte, payloader rtp.Payloader, audioFile string, wantInfo string) {
	recorder := NewRecorder(10 * time.Second)
	defer recorder.Close()

	epoch := time.Now()
	pushVideoFrames(recorder, frames, payloader, codec, epoch)
	pushAudio(t, recorder, audioFile, epoch)
	waitForRecorder(t, recorder)

	storageDir := t.TempDir()
	storage, err := NewLocalStorage(storageDir)
	require.NoError(t, err)
	service, err := NewService(storage, Config{BufferDuration: 10 * time.Second, DraftDirectory: filepath.Join(t.TempDir(), "drafts")})
	require.NoError(t, err)

	draft, err := service.CreateDraft("test-stream", recorder)
	require.NoError(t, err)

	// The buffer keeps 10 seconds, starting at a keyframe
	require.InDelta(t, 10, draft.DurationSeconds, 2.1)
	draftFrames, draftDuration, draftInfo := decodeClip(t, ffmpeg, filepath.Join(service.config.DraftDirectory, draft.ID+".mkv"))
	require.InDelta(t, draft.DurationSeconds, draftDuration, 0.5)
	require.InDelta(t, draft.DurationSeconds*testFPS, draftFrames, 2)
	require.Contains(t, draftInfo, wantInfo)
	require.Contains(t, draftInfo, "Audio: opus")

	// Start mid GOP: the clip starts at the preceding keyframe
	clip, err := service.Publish(context.Background(), draft.ID, 3.5, 7.5, "My clip ✂️")
	require.NoError(t, err)
	require.Equal(t, "My clip ✂️", clip.Title)
	require.Equal(t, uint(320), clip.Width)
	require.Equal(t, uint(240), clip.Height)

	clipFile := filepath.Join(storageDir, "test-stream", clip.ID+".mkv")
	if output := os.Getenv("CLIPS_TEST_OUTPUT"); output != "" {
		data, _ := os.ReadFile(clipFile)
		_ = os.WriteFile(filepath.Join(output, t.Name()+".mkv"), data, 0o644)
	}
	stat, err := os.Stat(clipFile)
	require.NoError(t, err)
	require.Equal(t, clip.SizeBytes, stat.Size())

	// 3.5s falls between keyframes, so the clip starts at the one before it
	clipFrames, clipDuration, clipInfo := decodeClip(t, ffmpeg, clipFile)
	require.GreaterOrEqual(t, clipDuration, 4.0)
	require.LessOrEqual(t, clipDuration, 6.1)
	require.InDelta(t, clipDuration*testFPS, clipFrames, 2)
	t.Logf("draft: %.2fs %d frames, clip: %.2fs %d frames, %d bytes", draftDuration, draftFrames, clipDuration, clipFrames, clip.SizeBytes)
	require.Contains(t, clipInfo, wantInfo)
	require.Contains(t, clipInfo, "title           : My clip ✂️")

	clips, err := service.List(context.Background(), "test-stream")
	require.NoError(t, err)
	require.Len(t, clips, 1)
	require.Equal(t, clip.ID, clips[0].ID)

	require.NoError(t, service.Delete(context.Background(), "test-stream", clip.ID))
	service.listCache = map[string]cachedList{}
	clips, err = service.List(context.Background(), "test-stream")
	require.NoError(t, err)
	require.Empty(t, clips)
}

func TestClipH264(t *testing.T) {
	ffmpeg := requireFFmpeg(t)
	videoFile, audioFile := encodeTestMedia(t, ffmpeg,
		"-c:v libx264 -profile:v high -bf 0 -g "+strconv.Itoa(testKeyframe)+" -keyint_min "+strconv.Itoa(testKeyframe)+" -sc_threshold 0 -pix_fmt yuv420p -bsf:v h264_mp4toannexb -f h264",
		"video.h264")

	file, err := os.Open(videoFile)
	require.NoError(t, err)
	defer file.Close()
	reader, err := h264reader.NewReader(file)
	require.NoError(t, err)

	// Group NAL units into access units: SPS/PPS/SEI belong to the following slice
	var frames [][]byte
	var current []byte
	for {
		nal, err := reader.NextNAL()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		current = append(current, 0, 0, 0, 1)
		current = append(current, nal.Data...)
		if nal.UnitType == h264reader.NalUnitTypeCodedSliceIdr || nal.UnitType == h264reader.NalUnitTypeCodedSliceNonIdr {
			frames = append(frames, current)
			current = nil
		}
	}
	require.Len(t, frames, testFPS*testSeconds)

	runClipTest(t, ffmpeg, codecs.VideoTrackCodecH264, frames, &pionCodecs.H264Payloader{}, audioFile, "Video: h264 (High)")
}

func TestClipAV1(t *testing.T) {
	ffmpeg := requireFFmpeg(t)
	videoFile, audioFile := encodeTestMedia(t, ffmpeg,
		"-c:v libaom-av1 -cpu-used 8 -usage realtime -b:v 300k -g "+strconv.Itoa(testKeyframe)+" -keyint_min "+strconv.Itoa(testKeyframe)+" -f ivf",
		"video.ivf")

	file, err := os.Open(videoFile)
	require.NoError(t, err)
	defer file.Close()
	reader, _, err := ivfreader.NewWith(file)
	require.NoError(t, err)

	var frames [][]byte
	for {
		frame, _, err := reader.ParseNextFrame()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		frames = append(frames, frame)
	}
	require.Len(t, frames, testFPS*testSeconds)

	runClipTest(t, ffmpeg, codecs.VideoTrackCodecAV1, frames, &pionCodecs.AV1Payloader{}, audioFile, "Video: av1")
}
