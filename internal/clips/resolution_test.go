package clips

import (
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/glimesh/broadcast-box/internal/webrtc/codecs"
	"github.com/pion/rtp"
	pionCodecs "github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4/pkg/media/h264reader"
	"github.com/pion/webrtc/v4/pkg/media/ivfreader"
	"github.com/stretchr/testify/require"
)

// Packetizes encoded video like a publisher would and checks the resolution
// is found in the keyframe packets
func findResolution(t *testing.T, codec codecs.TrackCodeType, payloader rtp.Payloader, frames [][]byte) (uint, uint) {
	for _, frame := range frames {
		for _, payload := range payloader.Payload(1200, frame) {
			if !codecs.IsKeyframe(payload, codec) {
				continue
			}
			if width, height, ok := ResolutionFromRTP(codec, payload); ok {
				return width, height
			}
		}
	}
	t.Fatal("no resolution found")
	return 0, 0
}

func TestResolutionFromRTP(t *testing.T) {
	ffmpeg := requireFFmpeg(t)
	dir := t.TempDir()
	source := "testsrc2=size=1280x720:rate=30:duration=1"

	h264File := filepath.Join(dir, "video.h264")
	ffmpegRun(t, ffmpeg, "-f", "lavfi", "-i", source, "-c:v", "libx264", "-bf", "0", "-bsf:v", "h264_mp4toannexb", "-f", "h264", h264File)
	file, err := os.Open(h264File)
	require.NoError(t, err)
	reader, err := h264reader.NewReader(file)
	require.NoError(t, err)
	var h264Frames [][]byte
	for {
		nal, err := reader.NextNAL()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		h264Frames = append(h264Frames, append([]byte{0, 0, 0, 1}, nal.Data...))
	}
	width, height := findResolution(t, codecs.VideoTrackCodecH264, &pionCodecs.H264Payloader{}, h264Frames)
	require.Equal(t, "1280x720", strconv.Itoa(int(width))+"x"+strconv.Itoa(int(height)))

	av1File := filepath.Join(dir, "video.ivf")
	ffmpegRun(t, ffmpeg, "-f", "lavfi", "-i", "testsrc2=size=854x480:rate=30:duration=1", "-c:v", "libaom-av1", "-cpu-used", "8", "-usage", "realtime", "-f", "ivf", av1File)
	file, err = os.Open(av1File)
	require.NoError(t, err)
	ivf, _, err := ivfreader.NewWith(file)
	require.NoError(t, err)
	var av1Frames [][]byte
	for {
		frame, _, err := ivf.ParseNextFrame()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		av1Frames = append(av1Frames, frame)
	}
	width, height = findResolution(t, codecs.VideoTrackCodecAV1, &pionCodecs.AV1Payloader{}, av1Frames)
	require.Equal(t, "854x480", strconv.Itoa(int(width))+"x"+strconv.Itoa(int(height)))
}
