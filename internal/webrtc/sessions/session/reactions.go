package session

import (
	"encoding/json"
	"sync"
	"time"
)

// Reactions are counted and sent to all viewers in one message per interval,
// so the number of messages stays constant regardless of how many viewers
// are clicking.
const reactionFlushInterval = 250 * time.Millisecond

// Emojis viewers can react with. Must match the reaction picker in the frontend.
var allowedReactions = map[string]bool{
	"❤️": true, "😂": true, "🔥": true, "👏": true, "😮": true,
	"🎉": true, "👍": true, "😢": true, "💯": true, "🙏": true,
}

const defaultReaction = "❤️"

type reactionAggregator struct {
	lock      sync.Mutex
	counts    map[string]int
	scheduled bool
}

type inboundDataMessage struct {
	Type  string `json:"type"`
	Emoji string `json:"emoji"`
}

type reactionsMessage struct {
	Type   string         `json:"type"`
	Counts map[string]int `json:"counts"`
}

// Handles a reaction message, returns false if the payload is not a reaction
func (s *Session) handleReactionMessage(payload []byte) bool {
	var message inboundDataMessage
	if err := json.Unmarshal(payload, &message); err != nil || message.Type != "reaction" {
		return false
	}

	emoji := message.Emoji
	if emoji == "" {
		emoji = defaultReaction
	}
	if !allowedReactions[emoji] {
		return true
	}

	s.reactions.lock.Lock()
	defer s.reactions.lock.Unlock()

	if s.reactions.counts == nil {
		s.reactions.counts = map[string]int{}
	}
	s.reactions.counts[emoji]++

	if !s.reactions.scheduled {
		s.reactions.scheduled = true
		time.AfterFunc(reactionFlushInterval, s.flushReactions)
	}

	return true
}

func (s *Session) flushReactions() {
	s.reactions.lock.Lock()
	counts := s.reactions.counts
	s.reactions.counts = nil
	s.reactions.scheduled = false
	s.reactions.lock.Unlock()

	if len(counts) == 0 {
		return
	}

	payload, err := json.Marshal(reactionsMessage{Type: "reactions", Counts: counts})
	if err != nil {
		return
	}

	s.broadcastDataChannel(payload)
}
