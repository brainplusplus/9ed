package visualstream

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// ── JpegTileDiffStrategy: computeChangedTiles ──

func TestJpegTileDiffFirstFrameAllTiles(t *testing.T) {
	strategy := NewJpegTileDiffStrategy()
	defer strategy.Close()

	// 512x512 image with 256px tiles = 4 tiles (2x2 grid)
	img := makeSolidImage(512, 512, color.RGBA{R: 128, G: 128, B: 128, A: 255})
	tiles := strategy.computeChangedTiles(img)
	if len(tiles) != 4 {
		t.Errorf("expected 4 tiles on first frame, got %d", len(tiles))
	}
}

func TestJpegTileDiffIdenticalFrameNoChanges(t *testing.T) {
	strategy := NewJpegTileDiffStrategy()
	defer strategy.Close()

	img := makeSolidImage(256, 256, color.RGBA{R: 128, G: 128, B: 128, A: 255})

	// First frame: 1 tile
	tiles1 := strategy.computeChangedTiles(img)
	if len(tiles1) != 1 {
		t.Fatalf("expected 1 tile on first frame, got %d", len(tiles1))
	}

	// Identical frame: 0 tiles (JPEG bytes match)
	tiles2 := strategy.computeChangedTiles(img)
	if len(tiles2) != 0 {
		t.Errorf("expected 0 changed tiles for identical frame, got %d", len(tiles2))
	}
}

func TestJpegTileDiffChangedFrameOnlyChanged(t *testing.T) {
	strategy := NewJpegTileDiffStrategy()
	defer strategy.Close()

	// 512x256 image with 256px tiles = 2 tiles (2x1 grid)
	img1 := makeSolidImage(512, 256, color.RGBA{R: 255, G: 0, B: 0, A: 255})
	tiles1 := strategy.computeChangedTiles(img1)
	if len(tiles1) != 2 {
		t.Fatalf("expected 2 tiles on first frame, got %d", len(tiles1))
	}

	// Change only the right tile (blue instead of red)
	img2 := makeSplitImage(512, 256, color.RGBA{R: 255, G: 0, B: 0, A: 255}, color.RGBA{R: 0, G: 0, B: 255, A: 255})
	tiles2 := strategy.computeChangedTiles(img2)
	if len(tiles2) != 1 {
		t.Errorf("expected 1 changed tile, got %d", len(tiles2))
	}
}

func TestJpegTileDiffTileCoordinates(t *testing.T) {
	strategy := NewJpegTileDiffStrategy()
	defer strategy.Close()

	// 300x300 with 256px tiles = 4 tiles: (0,0), (256,0), (0,256), (256,256)
	img := makeSolidImage(300, 300, color.RGBA{R: 50, G: 50, B: 50, A: 255})
	tiles := strategy.computeChangedTiles(img)
	if len(tiles) != 4 {
		t.Fatalf("expected 4 tiles, got %d", len(tiles))
	}

	// Verify tile coordinates cover the grid
	coords := make(map[string]bool)
	for _, tile := range tiles {
		coords[tileKey(tile.X/256, tile.Y/256)] = true
	}
	expected := []string{"0:0", "1:0", "0:1", "1:1"}
	for _, key := range expected {
		if !coords[key] {
			t.Errorf("expected tile at %s, not found", key)
		}
	}
}

func TestJpegTileDiffTileSizesAtEdges(t *testing.T) {
	strategy := NewJpegTileDiffStrategy()
	defer strategy.Close()

	// 300x300 with 256px tiles: edge tiles should be 44px (300-256)
	img := makeSolidImage(300, 300, color.RGBA{R: 50, G: 50, B: 50, A: 255})
	tiles := strategy.computeChangedTiles(img)

	for _, tile := range tiles {
		if tile.Width <= 0 || tile.Height <= 0 {
			t.Errorf("tile at (%d,%d) has invalid size %dx%d", tile.X, tile.Y, tile.Width, tile.Height)
		}
		if tile.X+tile.Width > 300 {
			t.Errorf("tile at (%d,%d) exceeds width: %d", tile.X, tile.Y, tile.X+tile.Width)
		}
		if tile.Y+tile.Height > 300 {
			t.Errorf("tile at (%d,%d) exceeds height: %d", tile.X, tile.Y, tile.Y+tile.Height)
		}
	}
}

// ── packTileMessage format ──

func TestPackTileMessageFormat(t *testing.T) {
	tile := tileData{
		X:         100,
		Y:         200,
		Width:     256,
		Height:    128,
		JPEGBytes: []byte{0xFF, 0xD8, 0xFF, 0xE0},
	}

	msg := packTileMessage(tile)

	expectedLen := 21 + len(tile.JPEGBytes)
	if len(msg) != expectedLen {
		t.Fatalf("expected message length %d, got %d", expectedLen, len(msg))
	}

	if msg[0] != 0x01 {
		t.Errorf("expected message type 0x01, got 0x%02x", msg[0])
	}

	x := binary.BigEndian.Uint32(msg[1:5])
	y := binary.BigEndian.Uint32(msg[5:9])
	w := binary.BigEndian.Uint32(msg[9:13])
	h := binary.BigEndian.Uint32(msg[13:17])
	jpegLen := binary.BigEndian.Uint32(msg[17:21])

	if x != 100 {
		t.Errorf("expected x=100, got %d", x)
	}
	if y != 200 {
		t.Errorf("expected y=200, got %d", y)
	}
	if w != 256 {
		t.Errorf("expected w=256, got %d", w)
	}
	if h != 128 {
		t.Errorf("expected h=128, got %d", h)
	}
	if jpegLen != uint32(len(tile.JPEGBytes)) {
		t.Errorf("expected jpegLen=%d, got %d", len(tile.JPEGBytes), jpegLen)
	}

	if !bytes.Equal(msg[21:], tile.JPEGBytes) {
		t.Error("JPEG payload does not match")
	}
}

func TestPackTileMessageEmptyJPEG(t *testing.T) {
	tile := tileData{
		X:         0,
		Y:         0,
		Width:     1,
		Height:    1,
		JPEGBytes: []byte{},
	}

	msg := packTileMessage(tile)
	if len(msg) != 21 {
		t.Errorf("expected 21 bytes for empty JPEG, got %d", len(msg))
	}
}

// ── extractSubImage ──

func TestExtractSubImageWithSubImager(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	sub := extractSubImage(img, image.Rect(10, 10, 50, 50))
	bounds := sub.Bounds()
	if bounds.Min.X != 10 || bounds.Min.Y != 10 || bounds.Max.X != 50 || bounds.Max.Y != 50 {
		t.Errorf("unexpected sub-image bounds: %v", bounds)
	}
}

func TestExtractSubImageFallbackForNonSubImager(t *testing.T) {
	// image.NewUniform does NOT implement subImager
	img := image.NewUniform(color.Black)
	sub := extractSubImage(img, image.Rect(0, 0, 10, 10))
	if sub == nil {
		t.Fatal("extractSubImage returned nil for non-SubImage type")
	}
	bounds := sub.Bounds()
	if bounds.Dx() != 10 || bounds.Dy() != 10 {
		t.Errorf("expected 10x10 fallback image, got %v", bounds)
	}
}

// ── decodeFrame ──

func TestDecodeFrameJPEG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 50}); err != nil {
		t.Fatal(err)
	}

	frame := Frame{Data: buf.Bytes(), Width: 64, Height: 64}
	decoded, err := decodeFrame(frame)
	if err != nil {
		t.Fatalf("decodeFrame failed for JPEG: %v", err)
	}
	if decoded.Bounds().Dx() != 64 {
		t.Errorf("expected width 64, got %d", decoded.Bounds().Dx())
	}
}

func TestDecodeFramePNG(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}

	frame := Frame{Data: buf.Bytes(), Width: 32, Height: 32}
	decoded, err := decodeFrame(frame)
	if err != nil {
		t.Fatalf("decodeFrame failed for PNG: %v", err)
	}
	if decoded.Bounds().Dx() != 32 {
		t.Errorf("expected width 32, got %d", decoded.Bounds().Dx())
	}
}

func TestDecodeFrameInvalid(t *testing.T) {
	frame := Frame{Data: []byte("not an image"), Width: 10, Height: 10}
	_, err := decodeFrame(frame)
	if err == nil {
		t.Error("expected error for invalid image data")
	}
}

// ── EncodeAndSend with nil DataChannel (should not panic) ──

func TestEncodeAndSendNilDataChannelNoPanic(t *testing.T) {
	strategy := NewJpegTileDiffStrategy()
	defer strategy.Close()

	frame := makeTestFrame(256, 256)
	peer := &PeerConnection{ID: "p1", DataChannel: nil} // nil DataChannel
	// Should not panic, should skip sending
	strategy.EncodeAndSend(frame, []*PeerConnection{peer})
}

func TestEncodeAndSendNoPeersNoPanic(t *testing.T) {
	strategy := NewJpegTileDiffStrategy()
	defer strategy.Close()

	frame := makeTestFrame(256, 256)
	strategy.EncodeAndSend(frame, nil)
}

func TestEncodeAndSendNilFrameDataNoPanic(t *testing.T) {
	strategy := NewJpegTileDiffStrategy()
	defer strategy.Close()

	peer := &PeerConnection{ID: "p1"}
	strategy.EncodeAndSend(Frame{}, []*PeerConnection{peer})
}

// ── InputEvent JSON round-trip ──

func TestInputEventJSONRoundTrip(t *testing.T) {
	evt := InputEvent{
		Type:   "mouse_click",
		X:      100.5,
		Y:      200.3,
		Button: 0,
	}

	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatal(err)
	}

	var decoded InputEvent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}

	if decoded.Type != evt.Type {
		t.Errorf("type mismatch: %s != %s", decoded.Type, evt.Type)
	}
	if decoded.X != evt.X {
		t.Errorf("x mismatch: %f != %f", decoded.X, evt.X)
	}
	if decoded.Y != evt.Y {
		t.Errorf("y mismatch: %f != %f", decoded.Y, evt.Y)
	}
	if decoded.Button != evt.Button {
		t.Errorf("button mismatch: %d != %d", decoded.Button, evt.Button)
	}
}

func TestInputEventOmitEmpty(t *testing.T) {
	evt := InputEvent{Type: "mouse_move", X: 10, Y: 20}
	data, err := json.Marshal(evt)
	if err != nil {
		t.Fatal(err)
	}

	// omitempty fields should not be present
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	if _, exists := raw["button"]; exists {
		t.Error("button should be omitted when zero")
	}
	if _, exists := raw["text"]; exists {
		t.Error("text should be omitted when empty")
	}
}

// ── Pixel-based MAD diff (VAL-VISUAL-001) ──

func TestPixelDiffStoresPrevFrameAsRGBA(t *testing.T) {
	strategy := NewJpegTileDiffStrategy()
	defer strategy.Close()

	img := makeSolidImage(256, 256, color.RGBA{R: 100, G: 100, B: 100, A: 255})
	_ = strategy.computeChangedTiles(img)

	strategy.mu.Lock()
	prev := strategy.prevFrameRGBA
	strategy.mu.Unlock()

	if prev == nil {
		t.Fatal("expected prevFrameRGBA to be set after first frame")
	}
	if prev.Bounds().Dx() != 256 || prev.Bounds().Dy() != 256 {
		t.Errorf("expected prevFrameRGBA 256x256, got %dx%d", prev.Bounds().Dx(), prev.Bounds().Dy())
	}
}

func TestPixelDiffBelowThresholdNotChanged(t *testing.T) {
	strategy := NewJpegTileDiffStrategy()
	defer strategy.Close()

	// First frame: solid color -> 1 tile sent
	img1 := makeSolidImage(256, 256, color.RGBA{R: 128, G: 128, B: 128, A: 255})
	tiles1 := strategy.computeChangedTiles(img1)
	if len(tiles1) != 1 {
		t.Fatalf("expected 1 tile on first frame, got %d", len(tiles1))
	}

	// Second frame: very slight change (MAD well below default threshold of ~3)
	// Each channel changes by 1 -> per-pixel channel diff = 1, MAD ~ 1
	img2 := makeSolidImage(256, 256, color.RGBA{R: 129, G: 128, B: 128, A: 255})
	tiles2 := strategy.computeChangedTiles(img2)
	if len(tiles2) != 0 {
		t.Errorf("expected 0 changed tiles for sub-threshold change (MAD below threshold), got %d", len(tiles2))
	}
}

func TestPixelDiffAboveThresholdChanged(t *testing.T) {
	strategy := NewJpegTileDiffStrategy()
	defer strategy.Close()

	// First frame
	img1 := makeSolidImage(256, 256, color.RGBA{R: 128, G: 128, B: 128, A: 255})
	_ = strategy.computeChangedTiles(img1)

	// Second frame: large change (MAD >> threshold) -> tile should be re-sent
	img2 := makeSolidImage(256, 256, color.RGBA{R: 200, G: 200, B: 200, A: 255})
	tiles2 := strategy.computeChangedTiles(img2)
	if len(tiles2) != 1 {
		t.Errorf("expected 1 changed tile for large change (MAD above threshold), got %d", len(tiles2))
	}
}

func TestPixelDiffOnlyChangedTilesSent(t *testing.T) {
	strategy := NewJpegTileDiffStrategy()
	defer strategy.Close()

	// 512x256 -> 2 tiles (2x1)
	img1 := makeSolidImage(512, 256, color.RGBA{R: 128, G: 128, B: 128, A: 255})
	_ = strategy.computeChangedTiles(img1)

	// Change only the right tile dramatically, leave left tile barely changed
	img2 := makeSplitImage(512, 256,
		color.RGBA{R: 128, G: 128, B: 128, A: 255},   // left: unchanged (MAD ~0)
		color.RGBA{R: 255, G: 255, B: 255, A: 255},   // right: big change
	)
	tiles2 := strategy.computeChangedTiles(img2)
	if len(tiles2) != 1 {
		t.Errorf("expected 1 changed tile (right only), got %d", len(tiles2))
	}
	if len(tiles2) > 0 && tiles2[0].X != 256 {
		t.Errorf("expected changed tile at X=256 (right tile), got X=%d", tiles2[0].X)
	}
}

func TestPixelDiffByteEqualFastPath(t *testing.T) {
	strategy := NewJpegTileDiffStrategy()
	defer strategy.Close()

	// Identical image -> byte-equal fast path should skip entirely
	img := makeSolidImage(256, 256, color.RGBA{R: 128, G: 128, B: 128, A: 255})
	tiles1 := strategy.computeChangedTiles(img)
	if len(tiles1) != 1 {
		t.Fatalf("expected 1 tile on first frame, got %d", len(tiles1))
	}

	// Same exact image -> fast-path byte equal, 0 tiles
	tiles2 := strategy.computeChangedTiles(img)
	if len(tiles2) != 0 {
		t.Errorf("expected 0 changed tiles for byte-equal frame, got %d", len(tiles2))
	}
}

// ── Per-tile MAD computation (VAL-VISUAL-001) ──

func TestComputeTileMADZeroForIdentical(t *testing.T) {
	a := image.NewRGBA(image.Rect(0, 0, 10, 10))
	b := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			a.SetRGBA(x, y, color.RGBA{R: 100, G: 50, B: 200, A: 255})
			b.SetRGBA(x, y, color.RGBA{R: 100, G: 50, B: 200, A: 255})
		}
	}

	mad := computeTileMAD(a, a.Bounds(), b, b.Bounds())
	if mad != 0 {
		t.Errorf("expected MAD=0 for identical tiles, got %f", mad)
	}
}

func TestComputeTileMADForChangedTile(t *testing.T) {
	a := image.NewRGBA(image.Rect(0, 0, 10, 10))
	b := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			a.SetRGBA(x, y, color.RGBA{R: 100, G: 100, B: 100, A: 255})
			b.SetRGBA(x, y, color.RGBA{R: 110, G: 100, B: 100, A: 255})
		}
	}

	mad := computeTileMAD(a, a.Bounds(), b, b.Bounds())
	// R differs by 10 on every pixel -> MAD should be 10 (R) + 0 (G) + 0 (B) averaged over 3 channels
	// = 10/3 per pixel, but mean absolute diff = sum(|di|)/n over all pixels*channels
	if mad <= 0 {
		t.Errorf("expected MAD > 0 for changed tile, got %f", mad)
	}
}

func TestComputeTileMADNoPrevReturnsMax(t *testing.T) {
	a := image.NewRGBA(image.Rect(0, 0, 10, 10))
	mad := computeTileMAD(a, a.Bounds(), nil, image.Rect(0, 0, 10, 10))
	if mad != madInf {
		t.Errorf("expected MAD=madInf when prev is nil, got %f", mad)
	}
}

// ── Adaptive quality tiers (VAL-VISUAL-002) ──

func TestSelectQualityTierHighEffectiveness(t *testing.T) {
	strategy := NewJpegTileDiffStrategy()
	defer strategy.Close()

	// effectiveness 1.0 -> highest tier (outputScale 1.0, jpegQuality 72)
	tier := strategy.selectQualityTier(1.0)
	if tier.outputScale != 1.0 || tier.jpegQuality != 72 {
		t.Errorf("high effectiveness (1.0): expected scale=1.0 quality=72, got scale=%v quality=%d", tier.outputScale, tier.jpegQuality)
	}
}

func TestSelectQualityTierMidEffectiveness(t *testing.T) {
	strategy := NewJpegTileDiffStrategy()
	defer strategy.Close()

	// effectiveness 0.7-0.99 -> second tier (outputScale 0.95, jpegQuality 62)
	tier := strategy.selectQualityTier(0.8)
	if tier.outputScale != 0.95 || tier.jpegQuality != 62 {
		t.Errorf("mid effectiveness (0.8): expected scale=0.95 quality=62, got scale=%v quality=%d", tier.outputScale, tier.jpegQuality)
	}
}

func TestSelectQualityTierLowEffectiveness(t *testing.T) {
	strategy := NewJpegTileDiffStrategy()
	defer strategy.Close()

	// effectiveness 0.4-0.69 -> third tier (outputScale 0.8, jpegQuality 52)
	tier := strategy.selectQualityTier(0.5)
	if tier.outputScale != 0.8 || tier.jpegQuality != 52 {
		t.Errorf("low effectiveness (0.5): expected scale=0.8 quality=52, got scale=%v quality=%d", tier.outputScale, tier.jpegQuality)
	}
}

func TestSelectQualityTierVeryLowEffectiveness(t *testing.T) {
	strategy := NewJpegTileDiffStrategy()
	defer strategy.Close()

	// effectiveness < 0.4 -> lowest tier (outputScale 0.65, jpegQuality 45)
	tier := strategy.selectQualityTier(0.1)
	if tier.outputScale != 0.65 || tier.jpegQuality != 45 {
		t.Errorf("very low effectiveness (0.1): expected scale=0.65 quality=45, got scale=%v quality=%d", tier.outputScale, tier.jpegQuality)
	}
}

func TestSelectQualityTierZeroEffectiveness(t *testing.T) {
	strategy := NewJpegTileDiffStrategy()
	defer strategy.Close()

	// effectiveness 0.0 -> lowest tier
	tier := strategy.selectQualityTier(0.0)
	if tier.outputScale != 0.65 || tier.jpegQuality != 45 {
		t.Errorf("zero effectiveness: expected scale=0.65 quality=45, got scale=%v quality=%d", tier.outputScale, tier.jpegQuality)
	}
}

func TestQualityTierOutputScaleRange(t *testing.T) {
	strategy := NewJpegTileDiffStrategy()
	defer strategy.Close()

	// Verify all tiers are within the specified ranges
	for _, tier := range strategy.tiers {
		if tier.outputScale < 0.65 || tier.outputScale > 1.0 {
			t.Errorf("outputScale %v out of range [0.65, 1.0]", tier.outputScale)
		}
		if tier.jpegQuality < 45 || tier.jpegQuality > 72 {
			t.Errorf("jpegQuality %d out of range [45, 72]", tier.jpegQuality)
		}
	}
}

// ── Dimension change clears prevTiles (VAL-VISUAL-010) ──

func TestDimensionChangeClearsPrevTiles(t *testing.T) {
	strategy := NewJpegTileDiffStrategy()
	defer strategy.Close()

	// First frame 256x256 -> 1 tile
	img1 := makeSolidImage(256, 256, color.RGBA{R: 128, G: 128, B: 128, A: 255})
	tiles1 := strategy.computeChangedTiles(img1)
	if len(tiles1) != 1 {
		t.Fatalf("expected 1 tile on first frame, got %d", len(tiles1))
	}

	// Frame with different dimensions -> should clear prev and send ALL tiles
	img2 := makeSolidImage(512, 512, color.RGBA{R: 128, G: 128, B: 128, A: 255})
	tiles2 := strategy.computeChangedTiles(img2)
	if len(tiles2) != 4 {
		t.Errorf("expected 4 tiles (all re-sent) after dimension change to 512x512, got %d", len(tiles2))
	}
}

func TestDimensionChangeClearsPrevFrameRGBA(t *testing.T) {
	strategy := NewJpegTileDiffStrategy()
	defer strategy.Close()

	img1 := makeSolidImage(256, 256, color.RGBA{R: 100, G: 100, B: 100, A: 255})
	_ = strategy.computeChangedTiles(img1)

	strategy.mu.Lock()
	oldFrame := strategy.prevFrameRGBA
	strategy.mu.Unlock()

	if oldFrame == nil {
		t.Fatal("expected prevFrameRGBA set after first frame")
	}

	// Process a frame with different dimensions
	img2 := makeSolidImage(128, 128, color.RGBA{R: 100, G: 100, B: 100, A: 255})
	_ = strategy.computeChangedTiles(img2)

	strategy.mu.Lock()
	newFrame := strategy.prevFrameRGBA
	strategy.mu.Unlock()

	if newFrame == nil {
		t.Fatal("expected prevFrameRGBA set after dimension change")
	}
	if newFrame.Bounds().Dx() != 128 || newFrame.Bounds().Dy() != 128 {
		t.Errorf("expected prevFrameRGBA updated to 128x128 after dimension change, got %dx%d",
			newFrame.Bounds().Dx(), newFrame.Bounds().Dy())
	}
}

func TestSameDimensionsDoesNotClear(t *testing.T) {
	strategy := NewJpegTileDiffStrategy()
	defer strategy.Close()

	// First frame
	img1 := makeSolidImage(256, 256, color.RGBA{R: 128, G: 128, B: 128, A: 255})
	_ = strategy.computeChangedTiles(img1)

	// Second frame, same dimensions, barely changed -> 0 tiles (not cleared)
	img2 := makeSolidImage(256, 256, color.RGBA{R: 129, G: 128, B: 128, A: 255})
	tiles2 := strategy.computeChangedTiles(img2)
	if len(tiles2) != 0 {
		t.Errorf("expected 0 tiles for sub-threshold change at same dims, got %d", len(tiles2))
	}
}

// ── Helpers ──

func makeSolidImage(w, h int, c color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func makeSplitImage(w, h int, left, right color.RGBA) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < w/2 {
				img.SetRGBA(x, y, left)
			} else {
				img.SetRGBA(x, y, right)
			}
		}
	}
	return img
}

func makeTestFrame(w, h int) Frame {
	img := makeSolidImage(w, h, color.RGBA{R: 128, G: 128, B: 128, A: 255})
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 50})
	return Frame{Data: buf.Bytes(), Width: w, Height: h}
}
