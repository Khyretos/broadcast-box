package clips

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// Codec specific parsing needed to store frames in Matroska: splitting and
// converting bitstreams, finding keyframes and building the CodecPrivate data.

var errShortBitstream = errors.New("bitstream too short")

type bitReader struct {
	data []byte
	pos  int // in bits
}

func (r *bitReader) bit() (uint, error) {
	if r.pos >= len(r.data)*8 {
		return 0, errShortBitstream
	}
	value := (r.data[r.pos/8] >> (7 - r.pos%8)) & 1
	r.pos++
	return uint(value), nil
}

func (r *bitReader) bits(n int) (uint, error) {
	var value uint
	for range n {
		bit, err := r.bit()
		if err != nil {
			return 0, err
		}
		value = value<<1 | bit
	}
	return value, nil
}

// Exp-Golomb coded unsigned integer (H264)
func (r *bitReader) ue() (uint, error) {
	leadingZeros := 0
	for {
		bit, err := r.bit()
		if err != nil {
			return 0, err
		}
		if bit == 1 {
			break
		}
		leadingZeros++
		if leadingZeros > 31 {
			return 0, errShortBitstream
		}
	}
	value, err := r.bits(leadingZeros)
	return (1<<leadingZeros - 1) + value, err
}

func (r *bitReader) se() (int, error) {
	value, err := r.ue()
	if value%2 == 1 {
		return int(value+1) / 2, err
	}
	return -int(value / 2), err
}

// Variable length unsigned integer (AV1 uvlc)
func (r *bitReader) uvlc() (uint, error) {
	leadingZeros := 0
	for {
		bit, err := r.bit()
		if err != nil {
			return 0, err
		}
		if bit == 1 {
			break
		}
		leadingZeros++
		if leadingZeros >= 32 {
			return 0, errShortBitstream
		}
	}
	value, err := r.bits(leadingZeros)
	return value + (1<<leadingZeros - 1), err
}

// H264

const (
	h264NALUTypeIDR = 5
	h264NALUTypeSPS = 7
	h264NALUTypePPS = 8
)

// Splits an Annex-B byte stream into NAL units
func splitAnnexB(data []byte) (nalus [][]byte) {
	start := -1
	for i := 0; i+2 < len(data); i++ {
		if data[i] != 0 || data[i+1] != 0 || data[i+2] != 1 {
			continue
		}

		if start >= 0 {
			end := i
			if end > start && data[end-1] == 0 {
				end-- // 4 byte start code
			}
			nalus = append(nalus, data[start:end])
		}
		start = i + 3
		i += 2
	}

	if start >= 0 && start < len(data) {
		nalus = append(nalus, data[start:])
	} else if start < 0 && len(data) > 0 {
		nalus = append(nalus, data)
	}

	return nalus
}

func h264IsKeyframe(data []byte) bool {
	for _, nalu := range splitAnnexB(data) {
		if len(nalu) > 0 && nalu[0]&0x1f == h264NALUTypeIDR {
			return true
		}
	}
	return false
}

// Converts an Annex-B access unit into length prefixed NAL units (AVCC)
func h264AnnexBToAVCC(data []byte) []byte {
	nalus := splitAnnexB(data)
	size := 0
	for _, nalu := range nalus {
		size += 4 + len(nalu)
	}

	out := make([]byte, 0, size)
	for _, nalu := range nalus {
		out = binary.BigEndian.AppendUint32(out, uint32(len(nalu)))
		out = append(out, nalu...)
	}
	return out
}

func h264ParameterSets(data []byte) (sps, pps []byte) {
	for _, nalu := range splitAnnexB(data) {
		if len(nalu) == 0 {
			continue
		}
		switch nalu[0] & 0x1f {
		case h264NALUTypeSPS:
			if sps == nil {
				sps = nalu
			}
		case h264NALUTypePPS:
			if pps == nil {
				pps = nalu
			}
		}
	}
	return sps, pps
}

// Removes emulation prevention bytes (00 00 03)
func removeEmulationPrevention(data []byte) []byte {
	out := make([]byte, 0, len(data))
	zeros := 0
	for _, b := range data {
		if zeros >= 2 && b == 3 {
			zeros = 0
			continue
		}
		out = append(out, b)
		if b == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return out
}

type h264SPSInfo struct {
	profile, constraints, level  byte
	chromaFormat                 uint
	bitDepthLuma, bitDepthChroma uint
	width, height                uint
}

func parseH264SPS(sps []byte) (info h264SPSInfo, err error) {
	if len(sps) < 4 {
		return info, errShortBitstream
	}
	info.profile, info.constraints, info.level = sps[1], sps[2], sps[3]
	info.chromaFormat = 1

	r := &bitReader{data: removeEmulationPrevention(sps[4:])}
	if _, err = r.ue(); err != nil { // seq_parameter_set_id
		return
	}

	switch info.profile {
	case 100, 110, 122, 244, 44, 83, 86, 118, 128, 138, 139, 134, 135:
		if info.chromaFormat, err = r.ue(); err != nil {
			return
		}
		if info.chromaFormat == 3 {
			if _, err = r.bit(); err != nil { // separate_colour_plane_flag
				return
			}
		}
		if info.bitDepthLuma, err = r.ue(); err != nil {
			return
		}
		if info.bitDepthChroma, err = r.ue(); err != nil {
			return
		}
		if _, err = r.bit(); err != nil { // qpprime_y_zero_transform_bypass_flag
			return
		}
		var scalingMatrixPresent uint
		if scalingMatrixPresent, err = r.bit(); err != nil {
			return
		}
		if scalingMatrixPresent == 1 {
			lists := 8
			if info.chromaFormat == 3 {
				lists = 12
			}
			for i := range lists {
				var present uint
				if present, err = r.bit(); err != nil {
					return
				}
				if present == 0 {
					continue
				}
				size := 16
				if i >= 6 {
					size = 64
				}
				lastScale, nextScale := 8, 8
				for range size {
					if nextScale != 0 {
						var delta int
						if delta, err = r.se(); err != nil {
							return
						}
						nextScale = (lastScale + delta + 256) % 256
					}
					if nextScale != 0 {
						lastScale = nextScale
					}
				}
			}
		}
	}

	if _, err = r.ue(); err != nil { // log2_max_frame_num_minus4
		return
	}
	var picOrderCntType uint
	if picOrderCntType, err = r.ue(); err != nil {
		return
	}
	switch picOrderCntType {
	case 0:
		if _, err = r.ue(); err != nil { // log2_max_pic_order_cnt_lsb_minus4
			return
		}
	case 1:
		if _, err = r.bit(); err != nil { // delta_pic_order_always_zero_flag
			return
		}
		if _, err = r.se(); err != nil { // offset_for_non_ref_pic
			return
		}
		if _, err = r.se(); err != nil { // offset_for_top_to_bottom_field
			return
		}
		var cycle uint
		if cycle, err = r.ue(); err != nil {
			return
		}
		for range cycle {
			if _, err = r.se(); err != nil {
				return
			}
		}
	}

	if _, err = r.ue(); err != nil { // max_num_ref_frames
		return
	}
	if _, err = r.bit(); err != nil { // gaps_in_frame_num_value_allowed_flag
		return
	}

	var widthInMbs, heightInMapUnits, frameMbsOnly uint
	if widthInMbs, err = r.ue(); err != nil {
		return
	}
	if heightInMapUnits, err = r.ue(); err != nil {
		return
	}
	if frameMbsOnly, err = r.bit(); err != nil {
		return
	}
	if frameMbsOnly == 0 {
		if _, err = r.bit(); err != nil { // mb_adaptive_frame_field_flag
			return
		}
	}
	if _, err = r.bit(); err != nil { // direct_8x8_inference_flag
		return
	}

	width := (widthInMbs + 1) * 16
	height := (2 - frameMbsOnly) * (heightInMapUnits + 1) * 16

	var cropping uint
	if cropping, err = r.bit(); err != nil {
		return
	}
	if cropping == 1 {
		var left, right, top, bottom uint
		for _, value := range []*uint{&left, &right, &top, &bottom} {
			if *value, err = r.ue(); err != nil {
				return
			}
		}

		cropX, cropY := uint(1), 2-frameMbsOnly
		switch info.chromaFormat {
		case 1:
			cropX, cropY = 2, 2*(2-frameMbsOnly)
		case 2:
			cropX, cropY = 2, 2-frameMbsOnly
		}
		width -= (left + right) * cropX
		height -= (top + bottom) * cropY
	}

	info.width, info.height = width, height
	return info, nil
}

// AVCDecoderConfigurationRecord for the Matroska CodecPrivate
func h264AVCC(sps, pps []byte, info h264SPSInfo) []byte {
	out := []byte{1, info.profile, info.constraints, info.level, 0xff, 0xe1}
	out = binary.BigEndian.AppendUint16(out, uint16(len(sps)))
	out = append(out, sps...)
	out = append(out, 1)
	out = binary.BigEndian.AppendUint16(out, uint16(len(pps)))
	out = append(out, pps...)

	switch info.profile {
	case 100, 110, 122, 144:
		out = append(out,
			0xfc|byte(info.chromaFormat&0x03),
			0xf8|byte(info.bitDepthLuma&0x07),
			0xf8|byte(info.bitDepthChroma&0x07),
			0)
	}
	return out
}

// AV1

const (
	av1OBUSequenceHeader    = 1
	av1OBUTemporalDelimiter = 2
	av1OBUPadding           = 15
)

type av1OBU struct {
	obuType byte
	data    []byte // complete OBU including header
	payload []byte
}

func readLEB128(data []byte) (value uint64, n int, err error) {
	for i := 0; i < 8 && i < len(data); i++ {
		value |= uint64(data[i]&0x7f) << (7 * i)
		if data[i]&0x80 == 0 {
			return value, i + 1, nil
		}
	}
	return 0, 0, errShortBitstream
}

// Splits a low overhead AV1 bitstream (OBUs with size fields)
func splitAV1OBUs(data []byte) (obus []av1OBU, err error) {
	for offset := 0; offset < len(data); {
		header := data[offset]
		headerSize := 1
		if header&0x04 != 0 { // obu_extension_flag
			headerSize = 2
		}
		if header&0x02 == 0 { // obu_has_size_field
			return obus, errors.New("av1 obu without size field")
		}
		if offset+headerSize > len(data) {
			return obus, errShortBitstream
		}

		size, n, err := readLEB128(data[offset+headerSize:])
		if err != nil {
			return obus, err
		}
		payloadStart := offset + headerSize + n
		end := payloadStart + int(size)
		if end > len(data) {
			return obus, errShortBitstream
		}

		obus = append(obus, av1OBU{
			obuType: (header >> 3) & 0x0f,
			data:    data[offset:end],
			payload: data[payloadStart:end],
		})
		offset = end
	}
	return obus, nil
}

func av1IsKeyframe(data []byte) bool {
	obus, _ := splitAV1OBUs(data)
	for _, obu := range obus {
		if obu.obuType == av1OBUSequenceHeader {
			return true
		}
	}
	return false
}

// Removes temporal delimiters and padding, which Matroska stores without
func av1StripOBUs(data []byte) []byte {
	obus, err := splitAV1OBUs(data)
	if err != nil {
		return data
	}

	out := make([]byte, 0, len(data))
	for _, obu := range obus {
		if obu.obuType != av1OBUTemporalDelimiter && obu.obuType != av1OBUPadding {
			out = append(out, obu.data...)
		}
	}
	return out
}

func av1SequenceHeader(data []byte) *av1OBU {
	obus, _ := splitAV1OBUs(data)
	for _, obu := range obus {
		if obu.obuType == av1OBUSequenceHeader {
			return &obu
		}
	}
	return nil
}

type av1SequenceInfo struct {
	profile, level, tier                  uint
	highBitDepth, twelveBit, monochrome   uint
	subsamplingX, subsamplingY, chromaPos uint
	width, height                         uint
}

//nolint:gocognit,cyclop // follows the sequence_header_obu syntax of the AV1 spec
func parseAV1SequenceHeader(payload []byte) (info av1SequenceInfo, err error) {
	r := &bitReader{data: payload}
	read := func(n int) uint {
		if err != nil {
			return 0
		}
		var value uint
		value, err = r.bits(n)
		return value
	}

	info.profile = read(3)
	read(1) // still_picture
	reducedStillPictureHeader := read(1)

	if reducedStillPictureHeader == 1 {
		info.level = read(5)
	} else {
		decoderModelInfoPresent := uint(0)
		bufferDelayLength := 0
		if timingInfoPresent := read(1); timingInfoPresent == 1 {
			read(32) // num_units_in_display_tick
			read(32) // time_scale
			if equalPictureInterval := read(1); equalPictureInterval == 1 && err == nil {
				_, err = r.uvlc()
			}
			if decoderModelInfoPresent = read(1); decoderModelInfoPresent == 1 {
				bufferDelayLength = int(read(5)) + 1
				read(32) // num_units_in_decoding_tick
				read(5)  // buffer_removal_time_length_minus_1
				read(5)  // frame_presentation_time_length_minus_1
			}
		}
		initialDisplayDelayPresent := read(1)
		operatingPoints := int(read(5)) + 1
		for i := range operatingPoints {
			read(12) // operating_point_idc
			level := read(5)
			tier := uint(0)
			if level > 7 {
				tier = read(1)
			}
			if i == 0 {
				info.level, info.tier = level, tier
			}
			if decoderModelInfoPresent == 1 {
				if decoderModelPresent := read(1); decoderModelPresent == 1 {
					read(bufferDelayLength) // decoder_buffer_delay
					read(bufferDelayLength) // encoder_buffer_delay
					read(1)                 // low_delay_mode_flag
				}
			}
			if initialDisplayDelayPresent == 1 {
				if present := read(1); present == 1 {
					read(4)
				}
			}
		}
	}

	widthBits := int(read(4)) + 1
	heightBits := int(read(4)) + 1
	info.width = read(widthBits) + 1
	info.height = read(heightBits) + 1

	if reducedStillPictureHeader == 0 {
		if frameIDNumbersPresent := read(1); frameIDNumbersPresent == 1 {
			read(4)
			read(3)
		}
	}
	read(1) // use_128x128_superblock
	read(1) // enable_filter_intra
	read(1) // enable_intra_edge_filter

	if reducedStillPictureHeader == 0 {
		read(1) // enable_interintra_compound
		read(1) // enable_masked_compound
		read(1) // enable_warped_motion
		read(1) // enable_dual_filter
		enableOrderHint := read(1)
		if enableOrderHint == 1 {
			read(1) // enable_jnt_comp
			read(1) // enable_ref_frame_mvs
		}
		forceScreenContentTools := uint(2)
		if chooseScreenContentTools := read(1); chooseScreenContentTools == 0 {
			forceScreenContentTools = read(1)
		}
		if forceScreenContentTools > 0 {
			if chooseIntegerMV := read(1); chooseIntegerMV == 0 {
				read(1) // seq_force_integer_mv
			}
		}
		if enableOrderHint == 1 {
			read(3) // order_hint_bits_minus_1
		}
	}

	read(1) // enable_superres
	read(1) // enable_cdef
	read(1) // enable_restoration

	// color_config
	info.highBitDepth = read(1)
	if info.profile == 2 && info.highBitDepth == 1 {
		info.twelveBit = read(1)
	}
	if info.profile != 1 {
		info.monochrome = read(1)
	}
	colorPrimaries, transfer, matrix := uint(2), uint(2), uint(2)
	if colorDescriptionPresent := read(1); colorDescriptionPresent == 1 {
		colorPrimaries, transfer, matrix = read(8), read(8), read(8)
	}

	switch {
	case info.monochrome == 1:
		info.subsamplingX, info.subsamplingY = 1, 1
	case colorPrimaries == 1 && transfer == 13 && matrix == 0:
		info.subsamplingX, info.subsamplingY = 0, 0
	default:
		read(1) // color_range
		switch info.profile {
		case 0:
			info.subsamplingX, info.subsamplingY = 1, 1
		case 1:
			info.subsamplingX, info.subsamplingY = 0, 0
		default:
			if info.twelveBit == 1 {
				info.subsamplingX = read(1)
				if info.subsamplingX == 1 {
					info.subsamplingY = read(1)
				}
			} else {
				info.subsamplingX, info.subsamplingY = 1, 0
			}
		}
		if info.subsamplingX == 1 && info.subsamplingY == 1 {
			info.chromaPos = read(2)
		}
	}

	return info, err
}

// AV1CodecConfigurationRecord for the Matroska CodecPrivate
func av1C(sequenceHeader []byte, info av1SequenceInfo) []byte {
	out := []byte{
		0x81,
		byte(info.profile<<5 | info.level&0x1f),
		byte(info.tier<<7 | info.highBitDepth<<6 | info.twelveBit<<5 | info.monochrome<<4 |
			info.subsamplingX<<3 | info.subsamplingY<<2 | info.chromaPos&0x03),
		0,
	}
	return append(out, sequenceHeader...)
}

// VP8 / VP9

func vp8IsKeyframe(data []byte) bool {
	return len(data) > 0 && data[0]&0x01 == 0
}

func vp8Dimensions(data []byte) (width, height uint) {
	// Keyframes: 3 byte frame tag, start code 9d 01 2a, then 14 bit width and height
	if !vp8IsKeyframe(data) || len(data) < 10 || !bytes.Equal(data[3:6], []byte{0x9d, 0x01, 0x2a}) {
		return 0, 0
	}
	return uint(binary.LittleEndian.Uint16(data[6:]) & 0x3fff), uint(binary.LittleEndian.Uint16(data[8:]) & 0x3fff)
}

type vp9FrameInfo struct {
	keyframe      bool
	width, height uint
}

func parseVP9Frame(data []byte) (info vp9FrameInfo) {
	r := &bitReader{data: data}
	read := func(n int) uint {
		value, _ := r.bits(n)
		return value
	}

	if read(2) != 2 { // frame_marker
		return info
	}
	profile := read(1) | read(1)<<1
	if profile == 3 {
		read(1)
	}
	if showExistingFrame := read(1); showExistingFrame == 1 {
		return info
	}
	if frameType := read(1); frameType != 0 {
		return info
	}
	info.keyframe = true

	read(1)                   // show_frame
	read(1)                   // error_resilient_mode
	if read(24) != 0x498342 { // frame_sync_code
		return info
	}
	if profile >= 2 {
		read(1) // ten_or_twelve_bit
	}
	if colorSpace := read(3); colorSpace != 7 { // not CS_RGB
		read(1) // color_range
		if profile == 1 || profile == 3 {
			read(3)
		}
	} else if profile == 1 || profile == 3 {
		read(1)
	}

	info.width = read(16) + 1
	info.height = read(16) + 1
	return info
}

// Opus

// OpusHead identification header for the Matroska CodecPrivate
func opusHead(channels byte) []byte {
	out := []byte("OpusHead")
	out = append(out, 1, channels)
	out = binary.LittleEndian.AppendUint16(out, 0) // pre-skip
	out = binary.LittleEndian.AppendUint32(out, 48000)
	out = binary.LittleEndian.AppendUint16(out, 0) // output gain
	return append(out, 0)                          // channel mapping family
}
