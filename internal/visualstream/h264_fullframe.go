package visualstream

import (
	"fmt"
	"io"
	"os/exec"
	"sync"

	"github.com/brainplusplus/9ed/internal/bininstall"
	"github.com/brainplusplus/9ed/internal/debug"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

// errClosed is returned when an operation is attempted on a closed strategy.
var errClosed = fmt.Errorf("h264 strategy closed")

// H264FullFrameStrategy implements Strategy B from ADR-0001: H264 full frame
// streaming via an ffmpeg subprocess that encodes raw RGBA/JPEG frames into
// H264 NAL units, distributed to peers via a pion video track.
//
// This strategy is suited for full-motion content (video, animations).
type H264FullFrameStrategy struct {
	mu        sync.Mutex
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	track     *webrtc.TrackLocalStaticSample
	width     int
	height    int
	framerate int
	bitrate   string
	preset    string
	closed    bool
}

// H264Config configures the ffmpeg encoder.
type H264Config struct {
	Width     int
	Height    int
	Framerate int
	Bitrate   string // e.g. "2M"
	Preset    string // e.g. "ultrafast"
}

// NewH264FullFrameStrategy creates a new H264 strategy. The strategy starts
// lazily on the first EncodeAndSend call once a video track is attached.
func NewH264FullFrameStrategy(cfg H264Config) *H264FullFrameStrategy {
	if cfg.Width <= 0 {
		cfg.Width = 1280
	}
	if cfg.Height <= 0 {
		cfg.Height = 800
	}
	if cfg.Framerate <= 0 {
		cfg.Framerate = 15
	}
	if cfg.Bitrate == "" {
		cfg.Bitrate = "2M"
	}
	if cfg.Preset == "" {
		cfg.Preset = "ultrafast"
	}
	return &H264FullFrameStrategy{
		width:     cfg.Width,
		height:    cfg.Height,
		framerate: cfg.Framerate,
		bitrate:   cfg.Bitrate,
		preset:    cfg.Preset,
	}
}

// SetTrack attaches a pion video track for H264 sample distribution.
func (s *H264FullFrameStrategy) SetTrack(track *webrtc.TrackLocalStaticSample) {
	s.mu.Lock()
	s.track = track
	s.mu.Unlock()
}

// EncodeAndSend encodes a frame via ffmpeg and writes H264 samples to the
// video track. If no track is attached, frames are silently dropped.
func (s *H264FullFrameStrategy) EncodeAndSend(frame Frame, peers []*PeerConnection) {
	if len(peers) == 0 || frame.Data == nil {
		return
	}

	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()

	if closed {
		return
	}

	// Ensure ffmpeg subprocess is running.
	if err := s.ensureEncoder(frame); err != nil {
		debug.Printf("[visualstream/h264] encoder error: %v", err)
		return
	}

	// Write raw frame bytes to ffmpeg stdin.
	// CDP screencast produces JPEG; ffmpeg can decode JPEG input via -i pipe:0.
	// For raw RGBA, we'd use -f rawvideo -pix_fmt rgba.
	s.mu.Lock()
	stdin := s.stdin
	s.mu.Unlock()

	if stdin == nil {
		return
	}

	if _, err := stdin.Write(frame.Data); err != nil {
		debug.Printf("[visualstream/h264] stdin write failed: %v", err)
	}
}

// ensureEncoder starts the ffmpeg subprocess on first call and adapts to the
// frame dimensions. If the ffmpeg binary is not found on the system, it is
// auto-downloaded via bininstall.FindBinary (platform-aware: Windows GyanD,
// Linux/macOS BtbN) per ADR-0001.
func (s *H264FullFrameStrategy) ensureEncoder(frame Frame) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errClosed
	}

	if s.cmd != nil && s.width == frame.Width && s.height == frame.Height {
		return nil
	}

	if s.cmd != nil {
		s.stopLocked()
	}

	if frame.Width > 0 {
		s.width = frame.Width
	}
	if frame.Height > 0 {
		s.height = frame.Height
	}

	// Locate ffmpeg binary — auto-downloads if missing (VAL-VISUAL-004).
	ffmpegPath, err := bininstall.FindBinary("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg not available: %w", err)
	}

	// ffmpeg reads JPEG frames from stdin and outputs raw H264 NAL units.
	args := []string{
		"-f", "image2pipe", "-vcodec", "mjpeg", "-i", "pipe:0",
		"-c:v", "libx264",
		"-preset", s.preset,
		"-tune", "zerolatency",
		"-b:v", s.bitrate,
		"-r", fmt.Sprintf("%d", s.framerate),
		"-f", "h264",
		"pipe:1",
	}

	cmd := exec.Command(ffmpegPath, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("ffmpeg stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return fmt.Errorf("ffmpeg stdout: %w", err)
	}
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return fmt.Errorf("ffmpeg start: %w", err)
	}

	s.cmd = cmd
	s.stdin = stdin
	s.stdout = stdout

	// Reader goroutine: reads H264 NAL units and writes samples to the track.
	go s.readSamples()

	debug.Printf("[visualstream/h264] ffmpeg started %dx%d@%dfps", s.width, s.height, s.framerate)
	return nil
}

// readSamples reads H264 Annex B data from ffmpeg stdout, parses NAL unit
// boundaries via start codes (00 00 00 01 / 00 00 01), and writes one
// WriteSample per NAL unit to the pion video track (VAL-VISUAL-003).
//
// A carryover buffer is maintained across reads so that NAL units and start
// codes split across read boundaries are handled correctly.
func (s *H264FullFrameStrategy) readSamples() {
	parser := newNALParser()
	buf := make([]byte, 65536)
	for {
		s.mu.Lock()
		stdout := s.stdout
		track := s.track
		s.mu.Unlock()

		if stdout == nil {
			return
		}

		n, err := stdout.Read(buf)

		if n > 0 {
			// Feed the new data into the Annex B parser, which splits at start
			// code boundaries and returns complete NAL payloads (without start
			// codes, as pion's H264Payloader expects).
			nals := parser.feed(buf[:n])
			if track != nil {
				for _, nal := range nals {
					if writeErr := track.WriteSample(media.Sample{Data: nal}); writeErr != nil {
						debug.Printf("[visualstream/h264] track write failed: %v", writeErr)
					}
				}
			}
		}

		if err != nil {
			// On EOF or error, flush any remaining NAL data in the carryover buffer.
			if track != nil {
				for _, nal := range parser.flush() {
					if writeErr := track.WriteSample(media.Sample{Data: nal}); writeErr != nil {
						debug.Printf("[visualstream/h264] track write failed (flush): %v", writeErr)
					}
				}
			}
			return
		}
	}
}

func (s *H264FullFrameStrategy) stopLocked() {
	if s.stdin != nil {
		_ = s.stdin.Close()
		s.stdin = nil
	}
	if s.stdout != nil {
		_ = s.stdout.Close()
		s.stdout = nil
	}
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_ = s.cmd.Wait()
		s.cmd = nil
	}
}

// Close stops the ffmpeg subprocess and releases resources.
func (s *H264FullFrameStrategy) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.stopLocked()
	return nil
}
