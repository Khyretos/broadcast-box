package codecs

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsKeyframeH264(t *testing.T) {
	require.True(t, IsKeyframe([]byte{0x67, 0x42}, VideoTrackCodecH264), "single SPS")
	require.True(t, IsKeyframe([]byte{0x65, 0x88}, VideoTrackCodecH264), "single IDR")
	require.False(t, IsKeyframe([]byte{0x41, 0x9a}, VideoTrackCodecH264), "single non-IDR slice")
	require.False(t, IsKeyframe([]byte{0x68, 0xce}, VideoTrackCodecH264), "PPS alone")

	stapA := []byte{0x78, 0x00, 0x02, 0x09, 0xf0, 0x00, 0x02, 0x67, 0x42}
	require.True(t, IsKeyframe(stapA, VideoTrackCodecH264), "STAP-A with SPS")

	require.True(t, IsKeyframe([]byte{0x7c, 0x85, 0x88}, VideoTrackCodecH264), "FU-A start of IDR")
	require.False(t, IsKeyframe([]byte{0x7c, 0x05, 0x88}, VideoTrackCodecH264), "FU-A middle of IDR")
	require.False(t, IsKeyframe([]byte{0x7c, 0x81, 0x88}, VideoTrackCodecH264), "FU-A start of non-IDR")
}

func TestIsKeyframeH265(t *testing.T) {
	require.True(t, IsKeyframe([]byte{32 << 1, 0x01, 0x0c}, VideoTrackCodecH265), "VPS")
	require.True(t, IsKeyframe([]byte{19 << 1, 0x01, 0xaf}, VideoTrackCodecH265), "IDR_W_RADL")
	require.False(t, IsKeyframe([]byte{1 << 1, 0x01, 0xaf}, VideoTrackCodecH265), "TRAIL_R")
	require.True(t, IsKeyframe([]byte{49 << 1, 0x01, 0x80 | 19}, VideoTrackCodecH265), "FU start of IDR")
	require.False(t, IsKeyframe([]byte{49 << 1, 0x01, 19}, VideoTrackCodecH265), "FU middle of IDR")
}

func TestIsKeyframeAV1(t *testing.T) {
	require.True(t, IsKeyframe([]byte{0x18, 0x00}, VideoTrackCodecAV1), "N bit set")
	require.False(t, IsKeyframe([]byte{0x10, 0x00}, VideoTrackCodecAV1), "N bit clear")
}

func TestIsKeyframeVP8(t *testing.T) {
	require.True(t, IsKeyframe([]byte{0x10, 0x00}, VideoTrackCodecVP8), "start, keyframe")
	require.False(t, IsKeyframe([]byte{0x10, 0x01}, VideoTrackCodecVP8), "start, interframe")
	require.False(t, IsKeyframe([]byte{0x00, 0x00}, VideoTrackCodecVP8), "not start of partition")
	require.True(t, IsKeyframe([]byte{0x90, 0x80, 0x81, 0x23, 0x00}, VideoTrackCodecVP8), "extended with 15 bit picture ID")
}

func TestIsKeyframeVP9(t *testing.T) {
	require.True(t, IsKeyframe([]byte{0x88}, VideoTrackCodecVP9), "I and B, not P")
	require.False(t, IsKeyframe([]byte{0xc8}, VideoTrackCodecVP9), "inter predicted")
	require.False(t, IsKeyframe([]byte{0x80}, VideoTrackCodecVP9), "not beginning of frame")
}
