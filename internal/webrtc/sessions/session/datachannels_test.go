package session

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDataChannelBroadcast(t *testing.T) {
	s := &Session{StreamKey: "stream-1"}

	// Register peers
	senderChannel := &fakeDataChannel{}
	recipientChannel := &fakeDataChannel{}
	failingRecipientChannel := &fakeDataChannel{sendTextError: errors.New("send failed")}
	otherStreamChannel := &fakeDataChannel{}

	sender := s.addDataChannelPeer("sender", senderChannel)
	recipient := s.addDataChannelPeer("recipient", recipientChannel)
	failingRecipient := s.addDataChannelPeer("failing-recipient", failingRecipientChannel)
	(&Session{StreamKey: "stream-2"}).addDataChannelPeer("other-stream", otherStreamChannel)

	// Text broadcasts
	s.broadcastDataChannelFrom(sender, []byte("hello"), true)
	assert.Empty(t, senderChannel.textMessages)
	assert.Empty(t, senderChannel.binaryMessages)
	assert.Equal(t, []string{"hello"}, recipientChannel.textMessages)
	assert.Empty(t, recipientChannel.binaryMessages)
	assert.Empty(t, otherStreamChannel.textMessages)
	assert.Empty(t, otherStreamChannel.binaryMessages)

	// Binary broadcasts
	s.broadcastDataChannelFrom(sender, []byte{0x01, 0x02, 0x03}, false)
	assert.Equal(t, [][]byte{{0x01, 0x02, 0x03}}, recipientChannel.binaryMessages)
	assert.Equal(t, []string{"hello"}, recipientChannel.textMessages)

	// Send failures
	assert.True(t, s.isDataChannelPeerRegistered(failingRecipient))
	s.broadcastDataChannelFrom(failingRecipient, []byte("still active"), true)
	assert.True(t, s.isDataChannelPeerRegistered(failingRecipient))
	assert.Equal(t, []string{"hello", "still active"}, recipientChannel.textMessages)

	// Unregister a peer
	s.removeDataChannelPeer(recipient)
	s.broadcastDataChannelFrom(sender, []byte("after unregister"), true)
	assert.Equal(t, []string{"hello", "still active"}, recipientChannel.textMessages)

	// Replace a peer
	oldChannel := &fakeDataChannel{}
	newChannel := &fakeDataChannel{}
	oldPeer := s.addDataChannelPeer("duplicate", oldChannel)
	newPeer := s.addDataChannelPeer("duplicate", newChannel)

	s.removeDataChannelPeer(oldPeer)
	assert.True(t, s.isDataChannelPeerRegistered(newPeer))
	s.broadcastDataChannelFrom(sender, []byte("replacement"), true)
	assert.Empty(t, oldChannel.textMessages)
	assert.Equal(t, []string{"replacement"}, newChannel.textMessages)
}

func (s *Session) isDataChannelPeerRegistered(peer *dataChannelPeer) bool {
	s.dataChannelPeersLock.RLock()
	defer s.dataChannelPeersLock.RUnlock()
	return s.dataChannelPeers[peer.id] == peer
}

type fakeDataChannel struct {
	lock           sync.Mutex
	textMessages   []string
	binaryMessages [][]byte
	sendTextError  error
	sendError      error
}

func (f *fakeDataChannel) texts() []string {
	f.lock.Lock()
	defer f.lock.Unlock()
	return append([]string(nil), f.textMessages...)
}

func (f *fakeDataChannel) Send(data []byte) error {
	f.lock.Lock()
	defer f.lock.Unlock()
	if f.sendError != nil {
		return f.sendError
	}

	f.binaryMessages = append(f.binaryMessages, append([]byte(nil), data...))
	return nil
}

func (f *fakeDataChannel) SendText(s string) error {
	f.lock.Lock()
	defer f.lock.Unlock()
	if f.sendTextError != nil {
		return f.sendTextError
	}

	f.textMessages = append(f.textMessages, s)
	return nil
}

func TestReactionsAreAggregated(t *testing.T) {
	s := &Session{StreamKey: "stream-1"}
	viewerA := &fakeDataChannel{}
	viewerB := &fakeDataChannel{}
	s.addDataChannelPeer("a", viewerA)
	s.addDataChannelPeer("b", viewerB)

	assert.True(t, s.handleReactionMessage([]byte(`{"type":"reaction","emoji":"🔥"}`)))
	assert.True(t, s.handleReactionMessage([]byte(`{"type":"reaction","emoji":"🔥"}`)))
	assert.True(t, s.handleReactionMessage([]byte(`{"type":"reaction"}`)))
	assert.True(t, s.handleReactionMessage([]byte(`{"type":"reaction","emoji":"<script>"}`)), "text is consumed but not counted")
	assert.False(t, s.handleReactionMessage([]byte(`{"type":"other"}`)))

	assert.Eventually(t, func() bool {
		return len(viewerA.texts()) == 1 && len(viewerB.texts()) == 1
	}, time.Second, 10*time.Millisecond)

	assert.JSONEq(t, `{"type":"reactions","counts":{"🔥":2,"❤️":1}}`, viewerA.texts()[0])
	assert.Equal(t, viewerA.texts(), viewerB.texts())
}

func TestReactionEmojiValidation(t *testing.T) {
	for _, emoji := range []string{"❤️", "🔥", "👍🏽", "🇳🇱", "1️⃣", "👨‍👩‍👧", "🏳️‍🌈", "🫡", "☕"} {
		assert.True(t, isReactionEmoji(emoji), emoji)
	}
	for _, text := range []string{"", "a", "hello", "1", "🔥 fire", "<b>", "🔥🔥🔥🔥🔥🔥🔥🔥🔥🔥🔥"} {
		assert.False(t, isReactionEmoji(text), text)
	}
}

func TestEmoteReactions(t *testing.T) {
	s := &Session{StreamKey: "stream-1"}
	viewer := &fakeDataChannel{}
	s.addDataChannelPeer("a", viewer)

	emote := `{"type":"reaction","emote":{"code":"catJAM","url":"https://cdn.7tv.app/emote/1/1x.webp"}}`
	assert.True(t, s.handleReactionMessage([]byte(emote)))
	assert.True(t, s.handleReactionMessage([]byte(emote)))
	assert.True(t, s.handleReactionMessage([]byte(`{"type":"reaction","emote":{"code":"evil","url":"https://evil.example.com/x.gif"}}`)))

	assert.Eventually(t, func() bool { return len(viewer.texts()) == 1 }, time.Second, 10*time.Millisecond)
	assert.JSONEq(t, `{"type":"reactions","counts":{},"emotes":[{"code":"catJAM","url":"https://cdn.7tv.app/emote/1/1x.webp","count":2}]}`, viewer.texts()[0])
}
