package visualstream

import (
	"bytes"
	"io"
	"testing"
)

// TestNALParserSingleNALUnit verifies that a single NAL unit preceded by a
// 4-byte start code is correctly extracted (without the start code). Since
// the stream is ongoing, feed() retains the trailing NAL as carryover until
// a second start code or flush finalizes it.
func TestNALParserSingleNALUnit(t *testing.T) {
	// [00 00 00 01] [NAL payload]
	data := []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x0a}
	parser := newNALParser()
	nals := parser.feed(data)

	if len(nals) != 0 {
		t.Fatalf("expected 0 NAL from feed (no following start code), got %d", len(nals))
	}

	nals = parser.flush()
	if len(nals) != 1 {
		t.Fatalf("expected 1 NAL after flush, got %d", len(nals))
	}
	expected := []byte{0x67, 0x42, 0x00, 0x0a}
	if !bytes.Equal(nals[0], expected) {
		t.Errorf("NAL data mismatch: got %v, want %v", nals[0], expected)
	}
}

// TestNALParserMultipleNALUnits verifies that multiple NAL units in a single
// feed are split correctly at start code boundaries. With 2 start codes,
// feed() returns the first NAL (bounded by both start codes). The second NAL
// (trailing, no following start code) is returned by flush().
func TestNALParserMultipleNALUnits(t *testing.T) {
	// Two NALs with 4-byte start codes
	data := []byte{
		0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x0a, // SPS
		0x00, 0x00, 0x00, 0x01, 0x68, 0xce, 0x38, 0x80, // PPS
	}
	parser := newNALParser()
	nals := parser.feed(data)

	if len(nals) != 1 {
		t.Fatalf("expected 1 NAL from feed, got %d", len(nals))
	}
	expected1 := []byte{0x67, 0x42, 0x00, 0x0a}
	if !bytes.Equal(nals[0], expected1) {
		t.Errorf("NAL 1 mismatch: got %v, want %v", nals[0], expected1)
	}

	nals = parser.flush()
	if len(nals) != 1 {
		t.Fatalf("expected 1 NAL after flush, got %d", len(nals))
	}
	expected2 := []byte{0x68, 0xce, 0x38, 0x80}
	if !bytes.Equal(nals[0], expected2) {
		t.Errorf("NAL 2 mismatch: got %v, want %v", nals[0], expected2)
	}
}

// TestNALParser3ByteStartCode verifies that 3-byte start codes (00 00 01)
// are correctly detected.
func TestNALParser3ByteStartCode(t *testing.T) {
	data := []byte{0x00, 0x00, 0x01, 0x65, 0x88, 0x80}
	parser := newNALParser()
	nals := parser.feed(data)

	if len(nals) != 0 {
		t.Fatalf("expected 0 NAL from feed, got %d", len(nals))
	}

	nals = parser.flush()
	if len(nals) != 1 {
		t.Fatalf("expected 1 NAL after flush with 3-byte start code, got %d", len(nals))
	}
	expected := []byte{0x65, 0x88, 0x80}
	if !bytes.Equal(nals[0], expected) {
		t.Errorf("NAL data mismatch: got %v, want %v", nals[0], expected)
	}
}

// TestNALParserMixedStartCodes verifies that 3-byte and 4-byte start codes
// can be intermixed.
func TestNALParserMixedStartCodes(t *testing.T) {
	data := []byte{
		0x00, 0x00, 0x00, 0x01, 0x67, // NAL 1: 4-byte start code
		0x00, 0x00, 0x01, 0x68, // NAL 2: 3-byte start code
	}
	parser := newNALParser()
	nals := parser.feed(data)

	if len(nals) != 1 {
		t.Fatalf("expected 1 NAL from feed (bounded by SC1 and SC2), got %d", len(nals))
	}
	if !bytes.Equal(nals[0], []byte{0x67}) {
		t.Errorf("NAL 1 mismatch: got %v", nals[0])
	}

	nals = parser.flush()
	if len(nals) != 1 {
		t.Fatalf("expected 1 NAL after flush, got %d", len(nals))
	}
	if !bytes.Equal(nals[0], []byte{0x68}) {
		t.Errorf("NAL 2 mismatch: got %v", nals[0])
	}
}

// TestNALParserCarryoverSplitAcrossReads verifies that the carryover buffer
// is maintained across reads. A NAL unit that is split across multiple feed
// calls (simulating reads from ffmpeg stdout) is correctly assembled.
func TestNALParserCarryoverSplitAcrossReads(t *testing.T) {
	parser := newNALParser()

	// First read: start code + partial payload
	read1 := []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x42}
	nals := parser.feed(read1)
	if len(nals) != 0 {
		t.Fatalf("expected 0 complete NALs after partial read, got %d", len(nals))
	}

	// Second read: rest of payload + new start code (which finalizes first NAL)
	read2 := []byte{0x00, 0x0a, 0x00, 0x00, 0x00, 0x01, 0x68, 0xce, 0x38, 0x80}
	nals = parser.feed(read2)
	if len(nals) != 1 {
		t.Fatalf("expected 1 complete NAL from first unit after second read, got %d", len(nals))
	}
	expected1 := []byte{0x67, 0x42, 0x00, 0x0a}
	if !bytes.Equal(nals[0], expected1) {
		t.Errorf("NAL 1 mismatch: got %v, want %v", nals[0], expected1)
	}

	// Flush to get the remaining NAL
	nals = parser.flush()
	if len(nals) != 1 {
		t.Fatalf("expected 1 NAL after flush, got %d", len(nals))
	}
	expected2 := []byte{0x68, 0xce, 0x38, 0x80}
	if !bytes.Equal(nals[0], expected2) {
		t.Errorf("NAL 2 mismatch: got %v, want %v", nals[0], expected2)
	}
}

// TestNALParserStartCodeSplitAcrossReads verifies that a start code itself
// can be split across reads (e.g. "00 00" at the end of one read, "00 01" at
// the start of the next).
func TestNALParserStartCodeSplitAcrossReads(t *testing.T) {
	parser := newNALParser()

	// First read: complete first NAL + start of next start code (00 00)
	read1 := []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x0a, 0x00, 0x00}
	nals := parser.feed(read1)
	if len(nals) != 0 {
		t.Fatalf("expected 0 NALs from first read (only 1 start code visible), got %d", len(nals))
	}

	// Second read: rest of start code (00 01) + payload
	read2 := []byte{0x00, 0x01, 0x68, 0xce}
	nals = parser.feed(read2)
	if len(nals) != 1 {
		t.Fatalf("expected 1 NAL from second read (first NAL now bounded), got %d", len(nals))
	}
	expected1 := []byte{0x67, 0x42, 0x00, 0x0a}
	if !bytes.Equal(nals[0], expected1) {
		t.Errorf("NAL 1 mismatch: got %v, want %v", nals[0], expected1)
	}

	// Flush to get the remaining NAL
	nals = parser.flush()
	if len(nals) != 1 {
		t.Fatalf("expected 1 NAL after flush, got %d", len(nals))
	}
	if !bytes.Equal(nals[0], []byte{0x68, 0xce}) {
		t.Errorf("NAL 2 mismatch: got %v", nals[0])
	}
}

// TestNALParserFlush returns pending data as a NAL unit (the final NAL in the
// stream that doesn't have a following start code).
func TestNALParserFlush(t *testing.T) {
	data := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x88, 0x80, 0x40}
	parser := newNALParser()
	nals := parser.feed(data)

	if len(nals) != 0 {
		t.Fatalf("expected 0 NALs from feed (no following start code), got %d", len(nals))
	}

	nals = parser.flush()
	if len(nals) != 1 {
		t.Fatalf("expected 1 NAL after flush, got %d", len(nals))
	}
	expected := []byte{0x65, 0x88, 0x80, 0x40}
	if !bytes.Equal(nals[0], expected) {
		t.Errorf("flush NAL mismatch: got %v, want %v", nals[0], expected)
	}
}

// TestNALParserEmptyInput ensures empty feed is safe.
func TestNALParserEmptyInput(t *testing.T) {
	parser := newNALParser()
	nals := parser.feed(nil)
	if len(nals) != 0 {
		t.Errorf("expected 0 NALs from empty feed, got %d", len(nals))
	}
	nals = parser.flush()
	if len(nals) != 0 {
		t.Errorf("expected 0 NALs from flush of empty parser, got %d", len(nals))
	}
}

// TestNALParserWriteSamplePerNAL verifies that the readSamples-style loop
// emits one WriteSample per NAL unit via the callback. This simulates how
// the H264 reader goroutine interacts with the NAL parser.
func TestNALParserWriteSamplePerNAL(t *testing.T) {
	data := []byte{
		0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x0a, // SPS
		0x00, 0x00, 0x00, 0x01, 0x68, 0xce, 0x38, 0x80, // PPS
		0x00, 0x00, 0x00, 0x01, 0x65, 0x88, 0x80, 0x40, // IDR slice
	}

	// Simulate chunked reads via a reader
	reader := bytes.NewReader(data)
	parser := newNALParser()
	buf := make([]byte, 16) // small buffer to force multiple reads

	var samples [][]byte
	collectSample := func(nalData []byte) {
		// Copy to avoid aliasing the parser's internal buffer
		cpy := make([]byte, len(nalData))
		copy(cpy, nalData)
		samples = append(samples, cpy)
	}

	for {
		n, err := reader.Read(buf)
		if n > 0 {
			nals := parser.feed(buf[:n])
			for _, nal := range nals {
				collectSample(nal)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected read error: %v", err)
		}
	}
	// Flush final NAL
	for _, nal := range parser.flush() {
		collectSample(nal)
	}

	if len(samples) != 3 {
		t.Fatalf("expected 3 samples (one per NAL), got %d", len(samples))
	}

	// Verify each NAL data is correct (without start code)
	expectedNALs := [][]byte{
		{0x67, 0x42, 0x00, 0x0a},
		{0x68, 0xce, 0x38, 0x80},
		{0x65, 0x88, 0x80, 0x40},
	}
	for i, expected := range expectedNALs {
		if !bytes.Equal(samples[i], expected) {
			t.Errorf("sample %d mismatch: got %v, want %v", i, samples[i], expected)
		}
	}
}

// TestNALParserNALWithoutStartCodePrefix verifies that data without any start
// code at the beginning is handled gracefully (accumulated, not emitted).
// ffmpeg output always starts with a start code, but the parser should be
// resilient.
func TestNALParserNALWithoutStartCodePrefix(t *testing.T) {
	parser := newNALParser()
	nals := parser.feed([]byte{0x67, 0x42, 0x00, 0x0a})
	if len(nals) != 0 {
		t.Fatalf("expected 0 NALs from data without start code prefix, got %d", len(nals))
	}
}
