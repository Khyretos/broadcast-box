package chatdc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidMessageEmotes(t *testing.T) {
	emotes := validMessageEmotes("hello catJAM world", map[string]string{
		"catJAM": "https://cdn.7tv.app/emote/1/1x.webp",
		"unused": "https://cdn.7tv.app/emote/2/1x.webp",
		"hello":  "https://evil.example.com/tracker.gif",
		"world":  "javascript:alert(1)",
	})
	require.Equal(t, map[string]string{"catJAM": "https://cdn.7tv.app/emote/1/1x.webp"}, emotes)
	require.Nil(t, validMessageEmotes("text", nil))
}
