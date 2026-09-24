package webrtc

import (
	"errors"
	"log/slog"
	"os"
	"strconv"

	"github.com/glimesh/broadcast-box/internal/environment"
	"github.com/glimesh/broadcast-box/internal/server/authorization"
	"github.com/glimesh/broadcast-box/internal/webrtc/codecs"
	"github.com/glimesh/broadcast-box/internal/webrtc/peerconnection"
	"github.com/glimesh/broadcast-box/internal/webrtc/sessions/manager"
	"github.com/glimesh/broadcast-box/internal/webrtc/utils"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// Returned when a stream has reached MAX_VIEWERS_PER_STREAM
var ErrStreamFull = errors.New("stream has reached its maximum number of viewers")

func WHEP(offer string, streamKey string) (string, string, error) {
	utils.DebugOutputOffer(offer)

	profile := authorization.PublicProfile{
		StreamKey: streamKey,
	}

	session, err := manager.SessionsManager.GetOrAddSession(profile, false)
	if err != nil {
		return "", "", err
	}

	if maxViewers := getMaxViewersPerStream(); maxViewers > 0 && session.GetStreamStatus().ViewerCount >= maxViewers {
		return "", "", ErrStreamFull
	}

	whepSessionID := uuid.New().String()

	peerConnection, err := peerconnection.CreateWHEPPeerConnection()
	if err != nil {
		return "", "", err
	}

	audioTrack, videoTrack := codecs.GetDefaultTracks(streamKey)

	_, err = peerConnection.AddTrack(audioTrack)
	if err != nil {
		return "", "", err
	}

	videoRTCPSender, err := peerConnection.AddTrack(videoTrack)
	if err != nil {
		return "", "", err
	}

	if err := peerConnection.SetRemoteDescription(webrtc.SessionDescription{
		SDP:  offer,
		Type: webrtc.SDPTypeOffer,
	}); err != nil {
		return "", "", err
	}

	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)
	answer, err := peerConnection.CreateAnswer(nil)

	if err != nil {
		return "", "", err
	} else if err = peerConnection.SetLocalDescription(answer); err != nil {
		return "", "", err
	}

	// TODO: Should this be before gatherComplete to assure registered events are triggered at correct time?
	if err := session.AddWHEP(
		whepSessionID,
		peerConnection,
		audioTrack,
		videoTrack,
		videoRTCPSender,
		func() {
			manager.SessionsManager.SendPLIByWHEPSessionID(whepSessionID)
		},
	); err != nil {
		return "", "", err
	}

	<-gatherComplete
	slog.Info("WHEPSession.GatheringCompletePromise: Completed Gathering", "streamKey", streamKey)

	return utils.DebugOutputAnswer(utils.AppendCandidateToAnswer(peerConnection.LocalDescription().SDP)),
		whepSessionID,
		nil
}

func getMaxViewersPerStream() int {
	maxViewers, err := strconv.Atoi(os.Getenv(environment.MaxViewersPerStream))
	if err != nil {
		return 0
	}
	return maxViewers
}
