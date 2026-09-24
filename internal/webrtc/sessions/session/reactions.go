package session

import (
	"encoding/json"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/glimesh/broadcast-box/internal/emotes"
)

// Reactions are counted and sent to all viewers in one message per interval,
// so the number of messages stays constant regardless of how many viewers
// are clicking.
const reactionFlushInterval = 250 * time.Millisecond

const (
	defaultReaction = "❤️"

	// Different emojis/emotes per batch, the rest of a burst is dropped
	maxReactionKinds = 32

	// Longest emoji sequence accepted, e.g. family or flag ZWJ sequences
	maxReactionRunes = 10
)

type reactionEmote struct {
	Code  string `json:"code"`
	URL   string `json:"url"`
	Count int    `json:"count"`
}

type reactionAggregator struct {
	lock      sync.Mutex
	counts    map[string]int
	emotes    map[string]*reactionEmote // by URL
	scheduled bool
}

type inboundDataMessage struct {
	Type  string `json:"type"`
	Emoji string `json:"emoji"`
	Emote *struct {
		Code string `json:"code"`
		URL  string `json:"url"`
	} `json:"emote"`
}

type reactionsMessage struct {
	Type   string           `json:"type"`
	Counts map[string]int   `json:"counts"`
	Emotes []*reactionEmote `json:"emotes,omitempty"`
}

// isReactionEmoji accepts a single emoji, including skin tones, flags,
// keycaps and ZWJ sequences, but no text
func isReactionEmoji(value string) bool {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxReactionRunes {
		return false
	}

	hasEmoji := false
	runes := []rune(value)
	for i, r := range runes {
		switch {
		case r == 0x20E3: // keycap, completes a digit keycap emoji
			hasEmoji = true
		case r == 0x200D, r == 0xFE0F, r == 0xFE0E: // ZWJ, variation selectors
		case r >= 0x1F3FB && r <= 0x1F3FF: // skin tones
		case r >= 0xE0020 && r <= 0xE007F: // tag sequences (subdivision flags)
		case (r >= '0' && r <= '9') || r == '#' || r == '*':
			// Only as the base of a keycap sequence
			if i+1 >= len(runes) || (runes[i+1] != 0xFE0F && runes[i+1] != 0x20E3) {
				return false
			}
		case unicode.Is(unicode.So, r), r >= 0x1F000 && r <= 0x1FAFF, r >= 0x2600 && r <= 0x27BF:
			hasEmoji = true
		default:
			return false
		}
	}
	return hasEmoji
}

// Handles a reaction message, returns false if the payload is not a reaction
func (s *Session) handleReactionMessage(payload []byte) bool {
	var message inboundDataMessage
	if err := json.Unmarshal(payload, &message); err != nil || message.Type != "reaction" {
		return false
	}

	s.reactions.lock.Lock()
	defer s.reactions.lock.Unlock()

	kinds := len(s.reactions.counts) + len(s.reactions.emotes)
	switch {
	case message.Emote != nil:
		if !emotes.IsValidCode(message.Emote.Code) || !emotes.IsAllowedImageURL(message.Emote.URL) {
			return true
		}
		if s.reactions.emotes == nil {
			s.reactions.emotes = map[string]*reactionEmote{}
		}
		emote, ok := s.reactions.emotes[message.Emote.URL]
		if !ok {
			if kinds >= maxReactionKinds {
				return true
			}
			emote = &reactionEmote{Code: message.Emote.Code, URL: message.Emote.URL}
			s.reactions.emotes[message.Emote.URL] = emote
		}
		emote.Count++

	default:
		emoji := message.Emoji
		if emoji == "" {
			emoji = defaultReaction
		}
		if !isReactionEmoji(emoji) {
			return true
		}
		if s.reactions.counts == nil {
			s.reactions.counts = map[string]int{}
		}
		if _, ok := s.reactions.counts[emoji]; !ok && kinds >= maxReactionKinds {
			return true
		}
		s.reactions.counts[emoji]++
	}

	if !s.reactions.scheduled {
		s.reactions.scheduled = true
		time.AfterFunc(reactionFlushInterval, s.flushReactions)
	}

	return true
}

func (s *Session) flushReactions() {
	s.reactions.lock.Lock()
	counts, emoteCounts := s.reactions.counts, s.reactions.emotes
	s.reactions.counts, s.reactions.emotes = nil, nil
	s.reactions.scheduled = false
	s.reactions.lock.Unlock()

	if len(counts) == 0 && len(emoteCounts) == 0 {
		return
	}

	message := reactionsMessage{Type: "reactions", Counts: counts}
	if message.Counts == nil {
		message.Counts = map[string]int{}
	}
	for _, emote := range emoteCounts {
		message.Emotes = append(message.Emotes, emote)
	}

	payload, err := json.Marshal(message)
	if err != nil {
		return
	}

	s.broadcastDataChannel(payload)
}
