package visualstream

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"sync"

	"github.com/brainplusplus/9ed/internal/debug"
)

// subImager is implemented by image types that support zero-copy sub-image
// extraction (e.g. *image.RGBA, *image.NRGBA, *image.YCbCr).
type subImager interface {
	SubImage(r image.Rectangle) image.Image
}

// extractSubImage returns the sub-image of img at rectangle r. If img does not
// support SubImage, it falls back to drawing onto a new NRGBA image.
func extractSubImage(img image.Image, r image.Rectangle) image.Image {
	if si, ok := img.(subImager); ok {
		return si.SubImage(r)
	}
	dst := image.NewNRGBA(r)
	draw.Draw(dst, r, img, r.Min, draw.Src)
	return dst
}

// JpegTileDiffStrategy implements Strategy A from ADR-0001: JPEG tile diff.
//
// It splits each frame into tiles (128-256px), compares with the previous
// frame, and sends only changed tiles as JPEG-encoded binary messages via
// pion DataChannel. This is the pola 9remote — efficient for mostly-static
// screen content (browser, desktop coding).
type JpegTileDiffStrategy struct {
	mu         sync.Mutex
	tileSize   int
	quality    int
	prevTiles  map[string][]byte // tile key -> previous JPEG bytes

	// Adaptive quality tiers (from 9remote config).
	tiers []qualityTier
}

type qualityTier struct {
	minEffective float64
	outputScale  float64
	jpegQuality  int
}

func NewJpegTileDiffStrategy() *JpegTileDiffStrategy {
	return &JpegTileDiffStrategy{
		tileSize:  256,
		quality:   60,
		prevTiles: make(map[string][]byte),
		tiers: []qualityTier{
			{minEffective: 1.0, outputScale: 1.0, jpegQuality: 72},
			{minEffective: 0.7, outputScale: 0.95, jpegQuality: 62},
			{minEffective: 0.4, outputScale: 0.8, jpegQuality: 52},
			{minEffective: 0.0, outputScale: 0.65, jpegQuality: 45},
		},
	}
}

// EncodeAndSend encodes a frame as JPEG tiles and sends changed tiles to all
// peers via DataChannel (ADR-0001 Strategy A).
func (s *JpegTileDiffStrategy) EncodeAndSend(frame Frame, peers []*PeerConnection) {
	if len(peers) == 0 || frame.Data == nil || frame.Width <= 0 || frame.Height <= 0 {
		return
	}

	// Decode raw bytes to image.Image based on assumed RGBA format.
	// CDP screencast produces JPEG/PNG, but we handle raw RGBA for flexibility.
	img, err := decodeFrame(frame)
	if err != nil {
		debug.Printf("[visualstream/jpeg] frame decode failed: %v", err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	changedTiles := s.computeChangedTiles(img)
	if len(changedTiles) == 0 {
		return // no changes
	}

	// Pack changed tiles into binary messages and send via DataChannel.
	for _, tile := range changedTiles {
		msg := packTileMessage(tile)
		for _, peer := range peers {
			if peer.DataChannel != nil {
				_ = peer.DataChannel.Send(msg)
			}
		}
	}
}

// Close clears the tile cache.
func (s *JpegTileDiffStrategy) Close() error {
	s.mu.Lock()
	s.prevTiles = make(map[string][]byte)
	s.mu.Unlock()
	return nil
}

type tileData struct {
	X, Y      int    // tile position in grid
	Width     int    // tile pixel width
	Height    int    // tile pixel height
	JPEGBytes []byte // encoded JPEG
}

func (s *JpegTileDiffStrategy) computeChangedTiles(img image.Image) []tileData {
	bounds := img.Bounds()
	cols := (bounds.Dx() + s.tileSize - 1) / s.tileSize
	rows := (bounds.Dy() + s.tileSize - 1) / s.tileSize

	var changed []tileData
	for row := 0; row < rows; row++ {
		for col := 0; col < cols; col++ {
			x := col * s.tileSize
			y := row * s.tileSize
			tileW := s.tileSize
			if x+tileW > bounds.Dx() {
				tileW = bounds.Dx() - x
			}
			tileH := s.tileSize
			if y+tileH > bounds.Dy() {
				tileH = bounds.Dy() - y
			}

			// Extract sub-image safely (handles images without SubImage).
			subImg := extractSubImage(img, image.Rect(x, y, x+tileW, y+tileH))

			// Encode as JPEG.
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, subImg, &jpeg.Options{Quality: s.quality}); err != nil {
				continue
			}

			tileKey := tileKey(col, row)
			prev, exists := s.prevTiles[tileKey]

			// Simple diff: compare JPEG bytes (if same, skip).
			if exists && bytes.Equal(prev, buf.Bytes()) {
				continue // tile unchanged
			}

			s.prevTiles[tileKey] = buf.Bytes()
			changed = append(changed, tileData{
				X: x, Y: y, Width: tileW, Height: tileH,
				JPEGBytes: buf.Bytes(),
			})
		}
	}
	return changed
}

// packTileMessage creates a binary message for a tile:
// [1 byte type] [4 bytes x] [4 bytes y] [4 bytes width] [4 bytes height] [4 bytes jpegLen] [jpeg bytes]
func packTileMessage(tile tileData) []byte {
	headerSize := 1 + 4*5
	msg := make([]byte, headerSize+len(tile.JPEGBytes))
	msg[0] = 0x01 // tile type
	binary.BigEndian.PutUint32(msg[1:5], uint32(tile.X))
	binary.BigEndian.PutUint32(msg[5:9], uint32(tile.Y))
	binary.BigEndian.PutUint32(msg[9:13], uint32(tile.Width))
	binary.BigEndian.PutUint32(msg[13:17], uint32(tile.Height))
	binary.BigEndian.PutUint32(msg[17:21], uint32(len(tile.JPEGBytes)))
	copy(msg[21:], tile.JPEGBytes)
	return msg
}

func tileKey(col, row int) string {
	return fmt.Sprintf("%d:%d", col, row)
}

// decodeFrame decodes raw frame bytes to an image.Image.
// Supports JPEG and PNG (from CDP screencast).
func decodeFrame(frame Frame) (image.Image, error) {
	reader := bytes.NewReader(frame.Data)
	img, _, err := image.Decode(reader)
	if err != nil {
		// Fallback: try as JPEG directly.
		reader.Reset(frame.Data)
		img, err = jpeg.Decode(reader)
		if err != nil {
			// Fallback: try as PNG.
			reader.Reset(frame.Data)
			img, err = png.Decode(reader)
			if err != nil {
				return nil, err
			}
		}
	}
	return img, nil
}
