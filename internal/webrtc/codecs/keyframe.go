package codecs

import "encoding/binary"

// IsKeyframe reports whether an RTP payload starts a frame that a decoder can
// start from, by inspecting the payload format headers only.
func IsKeyframe(payload []byte, codec TrackCodeType) bool {
	switch codec {
	case VideoTrackCodecH264:
		return isH264Keyframe(payload)
	case VideoTrackCodecH265:
		return isH265Keyframe(payload)
	case VideoTrackCodecAV1:
		return isAV1Keyframe(payload)
	case VideoTrackCodecVP8:
		return isVP8Keyframe(payload)
	case VideoTrackCodecVP9:
		return isVP9Keyframe(payload)
	}

	return false
}

const (
	h264NALUTypeIDR   = 5
	h264NALUTypeSPS   = 7
	h264NALUTypeSTAPA = 24
	h264NALUTypeFUA   = 28
)

// A decodable H264 stream starts at an SPS, or at an IDR for encoders that
// repeat parameter sets in band
func isH264KeyframeNALU(naluType byte) bool {
	return naluType == h264NALUTypeSPS || naluType == h264NALUTypeIDR
}

func isH264Keyframe(payload []byte) bool {
	if len(payload) < 2 {
		return false
	}

	switch naluType := payload[0] & 0x1f; naluType {
	case h264NALUTypeSTAPA:
		for offset := 1; offset+2 < len(payload); {
			size := int(binary.BigEndian.Uint16(payload[offset:]))
			offset += 2
			if isH264KeyframeNALU(payload[offset] & 0x1f) {
				return true
			}
			offset += size
		}
		return false
	case h264NALUTypeFUA:
		isStart := payload[1]&0x80 != 0
		return isStart && isH264KeyframeNALU(payload[1]&0x1f)
	default:
		return isH264KeyframeNALU(naluType)
	}
}

const (
	h265NALUTypeIRAPFirst = 16
	h265NALUTypeIRAPLast  = 21
	h265NALUTypeVPS       = 32
	h265NALUTypeAP        = 48
	h265NALUTypeFU        = 49
)

func isH265KeyframeNALU(naluType byte) bool {
	return naluType == h265NALUTypeVPS || (naluType >= h265NALUTypeIRAPFirst && naluType <= h265NALUTypeIRAPLast)
}

func isH265Keyframe(payload []byte) bool {
	if len(payload) < 3 {
		return false
	}

	switch naluType := (payload[0] >> 1) & 0x3f; naluType {
	case h265NALUTypeAP:
		for offset := 2; offset+2 < len(payload); {
			size := int(binary.BigEndian.Uint16(payload[offset:]))
			offset += 2
			if isH265KeyframeNALU((payload[offset] >> 1) & 0x3f) {
				return true
			}
			offset += size
		}
		return false
	case h265NALUTypeFU:
		isStart := payload[2]&0x80 != 0
		return isStart && isH265KeyframeNALU(payload[2]&0x3f)
	default:
		return isH265KeyframeNALU(naluType)
	}
}

// The N bit of the AV1 aggregation header marks the first packet of a new
// coded video sequence, which starts with a keyframe
func isAV1Keyframe(payload []byte) bool {
	return len(payload) > 0 && payload[0]&0x08 != 0
}

func isVP8Keyframe(payload []byte) bool {
	if len(payload) < 1 {
		return false
	}

	isStart := payload[0]&0x10 != 0
	partitionID := payload[0] & 0x0f
	if !isStart || partitionID != 0 {
		return false
	}

	offset := 1
	if payload[0]&0x80 != 0 { // X: extended control bits
		if len(payload) <= offset {
			return false
		}
		extension := payload[offset]
		offset++
		if extension&0x80 != 0 { // I: picture ID
			if len(payload) <= offset {
				return false
			}
			if payload[offset]&0x80 != 0 { // 15 bit picture ID
				offset++
			}
			offset++
		}
		if extension&0x40 != 0 { // L: TL0PICIDX
			offset++
		}
		if extension&0x30 != 0 { // T or K: TID/KEYIDX
			offset++
		}
	}

	// The P bit of the VP8 payload header is 0 for keyframes
	return len(payload) > offset && payload[offset]&0x01 == 0
}

// VP9: not inter predicted (P=0) and beginning of a frame (B=1)
func isVP9Keyframe(payload []byte) bool {
	return len(payload) > 0 && payload[0]&0x40 == 0 && payload[0]&0x08 != 0
}
