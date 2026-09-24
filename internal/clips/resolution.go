package clips

import (
	"encoding/binary"

	"github.com/glimesh/broadcast-box/internal/webrtc/codecs"
)

// ResolutionFromRTP reads the picture size from an RTP payload that carries
// codec configuration: an H264 SPS, an AV1 sequence header, a VP8 keyframe
// header or VP9 scalability structure. ok is false for other packets.
func ResolutionFromRTP(codec codecs.TrackCodeType, payload []byte) (width, height uint, ok bool) {
	switch codec {
	case codecs.VideoTrackCodecH264:
		if sps := h264SPSFromRTP(payload); sps != nil {
			if info, err := parseH264SPS(sps); err == nil && info.width > 0 {
				return info.width, info.height, true
			}
		}
	case codecs.VideoTrackCodecAV1:
		if sequenceHeader := av1SequenceHeaderFromRTP(payload); sequenceHeader != nil {
			if info, err := parseAV1SequenceHeader(sequenceHeader); err == nil && info.width > 0 {
				return info.width, info.height, true
			}
		}
	case codecs.VideoTrackCodecVP8:
		if data := vp8PayloadFromRTP(payload); data != nil {
			if width, height = vp8Dimensions(data); width > 0 {
				return width, height, true
			}
		}
	case codecs.VideoTrackCodecVP9:
		return vp9ResolutionFromRTP(payload)
	}
	return 0, 0, false
}

func h264SPSFromRTP(payload []byte) []byte {
	if len(payload) < 2 {
		return nil
	}

	switch payload[0] & 0x1f {
	case h264NALUTypeSPS:
		return payload
	case 24: // STAP-A
		for offset := 1; offset+2 < len(payload); {
			size := int(binary.BigEndian.Uint16(payload[offset:]))
			offset += 2
			if offset+size > len(payload) {
				return nil
			}
			if payload[offset]&0x1f == h264NALUTypeSPS {
				return payload[offset : offset+size]
			}
			offset += size
		}
	}
	return nil
}

// The sequence header is the first OBU element of a packet that starts a
// new coded video sequence
func av1SequenceHeaderFromRTP(payload []byte) []byte {
	if len(payload) < 3 || payload[0]&0x08 == 0 {
		return nil
	}

	element := payload[1:]
	if elementCount := (payload[0] >> 4) & 0x03; elementCount != 1 {
		size, n, err := readLEB128(element)
		if err != nil || n+int(size) > len(element) {
			return nil
		}
		element = element[n : n+int(size)]
	}

	if len(element) < 2 || (element[0]>>3)&0x0f != av1OBUSequenceHeader {
		return nil
	}
	headerSize := 1
	if element[0]&0x04 != 0 {
		headerSize = 2
	}
	obuPayload := element[headerSize:]
	if element[0]&0x02 != 0 { // obu_has_size_field
		size, n, err := readLEB128(obuPayload)
		if err != nil || n+int(size) > len(obuPayload) {
			return nil
		}
		obuPayload = obuPayload[n : n+int(size)]
	}
	return obuPayload
}

// Skips the VP8 payload descriptor of the first packet of a frame
func vp8PayloadFromRTP(payload []byte) []byte {
	if len(payload) < 1 || payload[0]&0x10 == 0 || payload[0]&0x0f != 0 {
		return nil
	}

	offset := 1
	if payload[0]&0x80 != 0 {
		if len(payload) <= offset {
			return nil
		}
		extension := payload[offset]
		offset++
		if extension&0x80 != 0 {
			if len(payload) <= offset {
				return nil
			}
			if payload[offset]&0x80 != 0 {
				offset++
			}
			offset++
		}
		if extension&0x40 != 0 {
			offset++
		}
		if extension&0x30 != 0 {
			offset++
		}
	}
	if offset >= len(payload) {
		return nil
	}
	return payload[offset:]
}

// Reads the first spatial layer's size from the VP9 scalability structure
func vp9ResolutionFromRTP(payload []byte) (width, height uint, ok bool) {
	if len(payload) < 1 || payload[0]&0x02 == 0 { // V: scalability structure present
		return 0, 0, false
	}

	flags := payload[0]
	offset := 1
	if flags&0x80 != 0 { // I: picture ID
		if len(payload) <= offset {
			return 0, 0, false
		}
		if payload[offset]&0x80 != 0 {
			offset++
		}
		offset++
	}
	if flags&0x20 != 0 { // L: layer indices
		offset++
		if flags&0x10 == 0 { // non flexible mode has TL0PICIDX
			offset++
		}
	}
	if flags&0x40 != 0 && flags&0x10 != 0 { // P and F: reference indices
		for offset < len(payload) {
			more := payload[offset]&0x01 != 0
			offset++
			if !more {
				break
			}
		}
	}

	if offset >= len(payload) || payload[offset]&0x10 == 0 { // Y: resolutions present
		return 0, 0, false
	}
	offset++
	if offset+4 > len(payload) {
		return 0, 0, false
	}
	return uint(binary.BigEndian.Uint16(payload[offset:])), uint(binary.BigEndian.Uint16(payload[offset+2:])), true
}
