package whep

import (
	"time"

	"github.com/glimesh/broadcast-box/internal/webrtc/codecs"
)

const (
	// About a second of video at high bitrates. A viewer that falls further
	// behind drops packets and resumes at the next keyframe.
	sendQueueSize = 2048

	pliInterval = 500 * time.Millisecond
)

// QueueVideoPacket queues a video packet for this viewer without blocking.
// The packet is shared between viewers and must not be modified.
func (w *WHEPSession) QueueVideoPacket(packet codecs.TrackPacket) {
	select {
	case w.sendQueue <- queuedPacket{packet: packet}:
	default:
		// Viewer can't keep up, skip ahead to the next keyframe so the
		// decoder never receives a broken frame
		w.VideoPacketsDropped.Add(1)
		w.IsWaitingForKeyframe.Store(true)
	}
}

// QueueAudioPacket queues an audio packet for this viewer without blocking.
// The packet is shared between viewers and must not be modified.
func (w *WHEPSession) QueueAudioPacket(packet codecs.TrackPacket) {
	select {
	case w.sendQueue <- queuedPacket{packet: packet, isAudio: true}:
	default:
	}
}

func (w *WHEPSession) runSender() {
	for {
		select {
		case <-w.closed:
			return
		case queued := <-w.sendQueue:
			// Copy the header, the viewer rewrites sequence numbers and timestamps
			packet := *queued.packet.Packet
			queued.packet.Packet = &packet

			if queued.isAudio {
				w.SendAudioPacket(queued.packet)
			} else {
				w.SendVideoPacket(queued.packet)
			}
		}
	}
}
