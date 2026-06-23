package visualstream

// This file implements an H264 Annex B NAL unit boundary parser.
//
// The parser scans byte streams for start codes (00 00 00 01 or 00 00 01),
// splits the stream into individual NAL units, and emits each NAL unit's
// payload (without the start code) via feed() and flush().
//
// A carryover buffer is maintained across feed() calls so that NAL units and
// start codes split across read boundaries are handled correctly.
//
// This replaces the raw Read+WriteSample approach in readSamples with proper
// Annex B parsing, as required by ADR-0001 (VAL-VISUAL-003).

// nalParser implements a streaming H264 Annex B NAL unit boundary parser.
//
// Usage:
//
//	parser := newNALParser()
//	for {
//	    n, _ := stdout.Read(buf)
//	    nals := parser.feed(buf[:n])   // returns complete NAL payloads
//	    for _, nal := range nals {
//	        track.WriteSample(media.Sample{Data: nal})
//	    }
//	}
//	// At EOF:
//	for _, nal := range parser.flush() {
//	    track.WriteSample(media.Sample{Data: nal})
//	}
type nalParser struct {
	// buf accumulates bytes that have not yet been bounded by a second start
	// code. It always starts at a start code boundary (or is empty).
	buf []byte
}

// newNALParser creates a new Annex B NAL parser with an empty carryover buffer.
func newNALParser() *nalParser {
	return &nalParser{}
}

// feed appends data to the parser's internal buffer and scans for complete
// NAL units. A NAL unit is complete when it is bounded by two start codes
// (or when flush() is called). Returns the payloads of all newly-completed
// NAL units (without start codes). The last NAL (not yet bounded by a
// following start code) remains in the internal buffer until the next feed
// or flush.
func (p *nalParser) feed(data []byte) [][]byte {
	if len(data) == 0 {
		return nil
	}
	p.buf = append(p.buf, data...)
	return p.extractComplete()
}

// flush emits any remaining buffered data as a final NAL unit. If the buffer
// is empty or contains only a start code prefix with no payload, nothing is
// emitted.
func (p *nalParser) flush() [][]byte {
	defer func() { p.buf = nil }()
	// Trim leading start code from the remaining buffer.
	payload := p.buf
	scLen := startCodeLen(payload)
	if scLen > 0 {
		payload = payload[scLen:]
	}
	// Trim trailing partial start code bytes (00 00 or 00) that might be the
	// beginning of a start code we never finished reading.
	payload = trimTrailingZeros(payload)
	if len(payload) == 0 {
		return nil
	}
	return [][]byte{payload}
}

// extractComplete scans p.buf for start code boundaries and extracts all
// fully-bounded NAL units. The last incomplete segment (after the final start
// code) is kept in p.buf as carryover.
func (p *nalParser) extractComplete() [][]byte {
	var nals [][]byte

	for {
		// Find the first start code in the buffer.
		sc1Start, sc1Len := findStartCode(p.buf, 0)
		if sc1Start < 0 || sc1Len == 0 {
			// No start code at all; keep everything as carryover.
			break
		}

		// Find the second start code after the first.
		sc2Start, _ := findStartCode(p.buf, sc1Start+sc1Len)
		if sc2Start < 0 {
			// No second start code yet. The first NAL is incomplete.
			// But we must retain up to 3 bytes before the end of buf as
			// potential start code prefix. However, since there's no second
			// start code, we can't emit anything yet.
			break
		}

		// Extract the NAL payload between the two start codes.
		payloadStart := sc1Start + sc1Len
		payload := p.buf[payloadStart:sc2Start]
		if len(payload) > 0 {
			cpy := make([]byte, len(payload))
			copy(cpy, payload)
			nals = append(nals, cpy)
		}

		// Advance past the first start code + payload, keeping from sc2Start onward.
		p.buf = append([]byte(nil), p.buf[sc2Start:]...)
	}

	return nals
}

// findStartCode searches buf starting at offset for the next Annex B start
// code (00 00 00 01 or 00 00 01). Returns the index of the start of the start
// code and its length (3 or 4), or (-1, 0) if not found.
func findStartCode(buf []byte, offset int) (int, int) {
	for i := offset; i < len(buf)-2; i++ {
		if buf[i] == 0 && buf[i+1] == 0 {
			if i+2 < len(buf) && buf[i+2] == 1 {
				return i, 3 // 00 00 01
			}
			if i+3 < len(buf) && buf[i+2] == 0 && buf[i+3] == 1 {
				return i, 4 // 00 00 00 01
			}
		}
	}
	return -1, 0
}

// startCodeLen returns the length of the start code at the beginning of buf
// (3 or 4), or 0 if buf does not start with a start code.
func startCodeLen(buf []byte) int {
	if len(buf) >= 4 && buf[0] == 0 && buf[1] == 0 && buf[2] == 0 && buf[3] == 1 {
		return 4
	}
	if len(buf) >= 3 && buf[0] == 0 && buf[1] == 0 && buf[2] == 1 {
		return 3
	}
	return 0
}

// trimTrailingZeros removes trailing 00 bytes that could be the beginning of
// a start code we never finished reading. This prevents partial start code
// prefixes from being included in NAL payloads during flush.
func trimTrailingZeros(buf []byte) []byte {
	end := len(buf)
	for end > 0 && buf[end-1] == 0 {
		end--
	}
	// Only trim if there are 1-2 trailing zeros (potential start code prefix).
	// If there are 3+ trailing zeros, they are actual data (unlikely but safe).
	trimmed := len(buf) - end
	if trimmed > 0 && trimmed <= 2 {
		return buf[:end]
	}
	return buf
}
