# 9ed Remote Desktop — Build From Scratch Design

**Date**: 2026-05-13
**Status**: Draft
**Approach**: C — Full custom implementation
**Estimated Effort**: 6-12 months (1 senior engineer)

---

## 1. Overview

This document specifies a from-scratch remote desktop module for 9ed. Every layer — screen capture, hardware encoding, WebRTC transport, input injection, session management — is built natively in Go with CGo bindings to platform APIs.

No external streaming server dependencies (Sunshine, Guacamole, etc). Single binary with optional CGo.

---

## 2. High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  Browser (9ed React)                                            │
│  ┌──────────────┐ ┌──────────────┐ ┌────────────────────────┐  │
│  │ RemoteViewer  │ │ RemoteToolbar│ │  RemoteStats           │  │
│  │ Canvas+WebRTC │ │ Quality,FS   │ │  FPS,RTT,Bitrate       │  │
│  └──────┬────────┘ └──────────────┘ └────────────────────────┘  │
│         │                                                        │
│         │ 3 channels:                                            │
│         │ ├── WebRTC Media (video H264)                         │
│         │ ├── WebRTC DataChannel (input: kb/mouse)              │
│         │ └── WebRTC DataChannel (control: stats/ping/reconnect)│
└─────────┼───────────────────────────────────────────────────────┘
          │ WebRTC (ICE/DTLS/SRTP)
          │
┌─────────┼───────────────────────────────────────────────────────┐
│  9ed Go Server                                                  │
│                                                                  │
│  ┌──────┴──────────────────────────────────────────────────┐    │
│  │                   Remote Module                          │    │
│  │                                                          │    │
│  │  ┌─────────────┐  ┌──────────────┐  ┌───────────────┐  │    │
│  │  │  Signaling   │  │   Session    │  │    Stream     │  │    │
│  │  │  Service     │  │   Manager    │  │    Manager    │  │    │
│  │  └─────────────┘  └──────────────┘  └───────┬───────┘  │    │
│  │                                              │          │    │
│  │  ┌───────────────────────────────────────────┴───────┐  │    │
│  │  │              Capture Pipeline                     │  │    │
│  │  │                                                    │  │    │
│  │  │  ┌──────────┐   ┌──────────┐   ┌──────────────┐  │  │    │
│  │  │  │  Screen   │──►│  Encoder  │──►│  RTP Packet  │  │  │    │
│  │  │  │  Capture  │   │  (GPU)   │   │ izer + Track │  │  │    │
│  │  │  └──────────┘   └──────────┘   └──────────────┘  │  │    │
│  │  │       ▲                              │            │  │    │
│  │  │       │          ┌──────────────┐    │            │  │    │
│  │  │       │          │  Adaptive    │◄───┘            │  │    │
│  │  │       │          │  Bitrate     │◄── stats        │  │    │
│  │  │       │          └──────────────┘                  │  │    │
│  │  └────────────────────────────────────────────────────┘  │    │
│  │                                                          │    │
│  │  ┌─────────────┐  ┌──────────────┐  ┌───────────────┐  │    │
│  │  │   Input      │  │  Clipboard   │  │    Audio      │  │    │
│  │  │   Injector   │  │   Sync       │  │   Capture     │  │    │
│  │  └─────────────┘  └──────────────┘  └───────────────┘  │    │
│  └─────────────────────────────────────────────────────────┘    │
│                                                                  │
│  ┌─────────────┐  ┌──────────────┐                              │
│  │   Config     │  │   SQLite     │                              │
│  │   Loader     │  │   Store      │                              │
│  └─────────────┘  └──────────────┘                              │
└──────────────────────────────────────────────────────────────────┘
```

---

## 3. Capture Pipeline — Detailed Design

### 3.1 Pipeline Stages

```
Stage 1: Capture        Stage 2: Encode         Stage 3: Transport
┌─────────────┐        ┌──────────────┐        ┌──────────────────┐
│ DXGI/X11/   │ frame  │ NVENC/       │ NAL    │ Pion WebRTC      │
│ Wayland/    │───────►│ QuickSync/   │───────►│ TrackLocal       │
│ Quartz      │ bits   │ VAAPI/       │ units  │ WriteSample()    │
│             │        │ VideoToolbox │        │                  │
└─────────────┘        └──────────────┘        └──────────────────┘
     goroutine              goroutine               goroutine
```

Each stage runs in its own goroutine. Frames flow through Go channels with backpressure.

### 3.2 Frame Data Structure

```go
// internal/remote/capture/types.go
package capture

import "image"

type Frame struct {
    ImageData  []byte      // Raw BGRA pixels (or texture handle)
    Width      int
    Height     int
    Stride     int
    DirtyRects []image.Rect // Changed regions (if available)
    Timestamp  int64        // Capture timestamp (nanoseconds)
    IsIDR      bool         // Force keyframe on this frame
}

type EncodedFrame struct {
    NALUnits  [][]byte     // H264 NAL units
    Timestamp uint32       // RTP timestamp (90kHz clock)
    IsKeyframe bool
    Width     int
    Height    int
}
```

### 3.3 Capture Interface

```go
// internal/remote/capture/capture.go
package capture

type CaptureBackend interface {
    // Start captures frames, sends to channel.
    // Blocks until Stop() called or error.
    Start(output chan<- Frame) error

    // Stop gracefully stops capture.
    Stop() error

    // SupportsDirtyRects returns true if backend provides dirty rects.
    SupportsDirtyRects() bool

    // DisplayName for UI.
    DisplayName() string
}
```

---

## 4. Windows Implementation (Phase 1)

### 4.1 DXGI Desktop Duplication

**C header wrapping strategy**: Minimal CGo wrapper. Only wrap the DXGI Desktop Duplication API surface we need.

```go
// internal/remote/capture/dxgi/dxgi.go
package dxgi

/*
#cgo CFLAGS: -DWIN32_LEAN_AND_MEAN
#cgo LDFLAGS: -ldxgi -ld3d11 -ldxguid

#include <windows.h>
#include <dxgi1_2.h>
#include <d3d11.h>

// Minimal wrapper functions to avoid exposing COM interfaces to Go
HRESULT dxgi_create_device(ID3D11Device** ppDevice);
HRESULT dxgi_duplicate_output(ID3D11Device* pDevice, IDXGIOutputDuplication** ppDup);
HRESULT dxgi_acquire_frame(IDXGIOutputDuplication* pDup,
                           ID3D11Texture2D** ppTexture,
                           DXGI_OUTDUPL_FRAME_INFO* pInfo,
                           RECT* dirtyRects, UINT* dirtyRectsCount, UINT maxDirtyRects);
void dxgi_release_frame(IDXGIOutputDuplication* pDup);
HRESULT dxgi_copy_texture(ID3D11Device* pDevice, ID3D11Texture2D* pSrc,
                          void* pDst, UINT dstStride, UINT height);
*/
import "C"
```

**Capture loop** (goroutine):

```go
func (d *DXGICapture) Start(output chan<- capture.Frame) error {
    // 1. Create D3D11 device
    // 2. Get DXGI adapter → output → duplicate output
    // 3. Loop:
    //    a. AcquireNextFrame(timeout=16ms for 60fps)
    //    b. Copy GPU texture → CPU memory (staging texture)
    //    c. Extract dirty rects from DXGI_OUTDUPL_FRAME_INFO
    //    d. Build Frame struct, send to channel
    //    e. Release frame
}
```

**Latency budget for capture**:
- AcquireNextFrame: ~0.5ms (GPU wait)
- Texture copy (GPU→CPU staging): ~1-2ms
- Total: ~1.5-2.5ms

**Key design decisions**:
- GPU→CPU copy is unavoidable with Pion WebRTC (no GPU memory API). Accept this cost.
- Staging texture used for async copy — doesn't block GPU.
- Dirty rects extracted from `DXGI_OUTDUPL_FRAME_INFO` for future optimization.

### 4.2 NVENC Hardware Encoding

**Strategy**: Wrap minimal NVENC API surface via CGo. ~15 functions needed.

```go
// internal/remote/encode/nvenc/nvenc.go
package nvenc

/*
#cgo CFLAGS: -I${SRCDIR}/include
#cgo windows LDFLAGS: -lnvEncodeAPI64

#include "nvEncodeAPI.h"

// Wrapper to avoid exposing NVENC structs directly to Go
typedef struct {
    void* session;
    void* encoder;
} NvencContext;

NvencContext nvenc_create_session(int width, int height, int fps, int bitrate, int gopLength);
NVENCSTATUS nvenc_encode_frame(NvencContext ctx, void* frameData, int stride,
                               void** outputNALs, int* outputSizes, int* outputCount,
                               int forceIDR);
void nvenc_destroy_session(NvencContext ctx);
*/
import "C"
```

**Encoder configuration**:

```go
type EncoderConfig struct {
    Width       int     // 1920
    Height      int     // 1080
    FPS         int     // 60
    Bitrate     int     // 12000000 (12 Mbps)
    GOPLength   int     // 60 (1 keyframe per second at 60fps)
    Profile     string  // "high" (H264 High profile)
    RateControl string  // "cbr" (constant bitrate for smooth streaming)
    preset      string  // "p1" (lowest latency preset) or "p4" (balanced)
    Tune        string  // "ultralowlatency"
    MaxQP       int     // 51
    MinQP       int     // 18
}
```

**Encode loop** (goroutine):

```go
func (e *NVENCEncoder) Start(input <-chan capture.Frame, output chan<- capture.EncodedFrame) error {
    for frame := range input {
        // 1. Pass frame BGRA data to NVENC via CGo
        // 2. nvenc_encode_frame() — returns NAL units
        // 3. Build EncodedFrame with NAL units + RTP timestamp
        // 4. Send to output channel
    }
}
```

**Latency budget for encode**:
- NVENC with `preset=p1, tune=ultralowlatency`: ~2-5ms per frame
- B-frames disabled (latency requirement)
- Weighted prediction disabled
- Output delay = 0 (no reordering)

**NVENC session limits**:
- Consumer GPUs (GeForce): 3 concurrent sessions
- Professional GPUs (Quadro/Tesla): Unlimited
- **Mitigation**: Check session count. Return clear error if exceeded.

### 4.3 Software Fallback Encoder

When no GPU available, fall back to software encoding.

**Option A: x264 via CGo** (`libx264`)
- Latency: ~15-30ms per frame (1080p)
- Quality: Excellent
- License: GPL (problematic)

**Option B: OpenH264 via CGo** (`libopenh264`)
- Latency: ~20-40ms per frame
- Quality: Good (no B-frames in baseline)
- License: BSD (safe)

**Recommended**: OpenH264 as fallback. GPL avoids license contamination.

```go
// internal/remote/encode/openh264/openh264.go
// CGo wrapper around libopenh264
```

### 4.4 Input Injection (Windows)

```go
// internal/remote/input/windows/input.go
package windows

/*
#cgo LDFLAGS: -luser32

#include <windows.h>

void inject_key_down(UINT vk, UINT scancode);
void inject_key_up(UINT vk, UINT scancode);
void inject_mouse_move(int x, int y);
void inject_mouse_down(int button);
void inject_mouse_up(int button);
void inject_mouse_wheel(int delta);
*/
import "C"
```

**Input event types**:

```go
type InputEvent struct {
    Type      string    `json:"type"`       // "keydown", "keyup", "mousemove", "mousedown", "mouseup", "wheel"
    Key       string    `json:"key,omitempty"`
    KeyCode   int       `json:"keyCode,omitempty"`
    Button    int       `json:"button,omitempty"`  // 0=left, 1=middle, 2=right
    X         int       `json:"x,omitempty"`
    Y         int       `json:"y,omitempty"`
    DeltaX    int       `json:"deltaX,omitempty"`
    DeltaY    int       `json:"deltaY,omitempty"`
    Modifiers []string  `json:"modifiers,omitempty"` // "ctrl", "alt", "shift", "meta"
    Timestamp int64     `json:"timestamp"`
}
```

**Latency**: SendInput is near-instant (~0.1ms). Input is the highest-priority channel.

---

## 5. Linux Implementation (Phase 2)

### 5.1 Screen Capture

```go
// internal/remote/capture/x11/x11.go — X11/Damage extension
// internal/remote/capture/wayland/wayland.go — PipeWire portal (xdg-desktop-portal)
```

**X11 capture** (XGetImage / XShmGetImage + Damage extension):
- XShmGetImage for fast shared-memory capture
- XDamage extension for dirty region hints
- CGo wrapping: `XShmGetImage`, `XDamageCreate`, `XDamageSubtract`

**Wayland capture** (PipeWire + xdg-desktop-portal):
- More complex — requires D-Bus negotiation with compositor
- Portal API: `org.freedesktop.portal.ScreenCast`
- Returns PipeWire fd → read frames from PipeWire stream
- CGo wrapping: `pw_stream_connect`, `pw_stream_queue_buffer`

**Strategy**: Try Wayland/PipeWire first. Fall back to X11/XCB if Wayland unavailable.

### 5.2 Hardware Encoding

```go
// internal/remote/encode/vaapi/vaapi.go — Intel/AMD via VAAPI
// internal/remote/encode/nvenc/nvenc_linux.go — NVIDIA via NVENC (same API, Linux build)
```

**VAAPI** (Intel/AMD):
- CGo wrapping: `vaInitialize`, `vaCreateContext`, `vaBeginPicture`, `vaRenderPicture`
- Or: Use libavcodec via CGo with VAAPI hardware acceleration — simpler API surface

### 5.3 Input Injection

```go
// internal/remote/input/x11/input.go — XTest extension
// XTestFakeKeyEvent, XTestFakeMotionEvent, XTestFakeButtonEvent
```

---

## 6. macOS Implementation (Phase 2)

### 6.1 Screen Capture

```go
// internal/remote/capture/quartz/quartz.go — CGWindowListCreateImage
// or: ScreenCaptureKit (macOS 12.3+)
```

- `CGWindowListCreateImage` via CGo — simpler but older API
- ScreenCaptureKit — modern, async, but requires Swift bridging

### 6.2 Hardware Encoding

```go
// internal/remote/encode/videotoolbox/videotoolbox.go
// VTCompressionSessionCreate, VTCompressionSessionEncodeFrame
```

- VideoToolbox H264 encoder — available on all macOS hardware
- CGo wrapping via Objective-C runtime headers

### 6.3 Input Injection

```go
// internal/remote/input/quartz/input.go
// CGEventCreateKeyboardEvent, CGEventCreateMouseEvent, CGEventPost
```

---

## 7. WebRTC Transport Layer

### 7.1 Pion WebRTC Integration

```go
// internal/remote/webrtc/peer.go
package webrtc

import (
    "github.com/pion/webrtc/v3"
    "github.com/pion/webrtc/v3/pkg/media"
)

type PeerConnection struct {
    pc         *webrtc.PeerConnection
    videoTrack *webrtc.TrackLocalStaticSample
    inputDC    *webrtc.DataChannel  // Reliable ordered — input events
    controlDC  *webrtc.DataChannel  // Reliable ordered — control/stats
    fileDC     *webrtc.DataChannel  // Reliable ordered — file transfer (Phase 2)
    clipDC     *webrtc.DataChannel  // Reliable ordered — clipboard (Phase 2)
}
```

### 7.2 Video Track Creation

```go
func createVideoTrack(pc *webrtc.PeerConnection) (*webrtc.TrackLocalStaticSample, error) {
    // H264 codec registration (Pion has built-in H264 support)
    track, err := webrtc.NewTrackLocalStaticSample(
        webrtc.RTPCodecCapability{
            MimeType:  webrtc.MimeTypeH264,
            ClockRate: 90000,
        },
        "video", "9ed-remote-desktop",
    )
    if err != nil {
        return nil, err
    }

    // Add track to peer connection — triggers negotiation
    if err := pc.AddTrack(track); err != nil {
        return nil, err
    }

    return track, nil
}
```

### 7.3 NAL Unit Injection

```go
func writeH264Frame(track *webrtc.TrackLocalSample, frame capture.EncodedFrame) error {
    // Concatenate NAL units with Annex-B start codes
    var buf []byte
    for _, nal := range frame.NALUnits {
        buf = append(buf, []byte{0x00, 0x00, 0x00, 0x01}...)
        buf = append(buf, nal...)
    }

    // Pion handles RTP packetization internally
    // (fragmentation into MTU-sized packets, timestamp mapping)
    return track.WriteSample(media.Sample{
        Data:     buf,
        Duration: time.Second / time.Duration(60), // frame duration
    })
}
```

**Key detail**: Pion's `WriteSample` handles:
- NAL unit → RTP packet fragmentation (FU-A for large NALs)
- Timestamp conversion (Duration → RTP timestamp)
- Sequence numbering

### 7.4 Keyframe Requests (PLI Handler)

```go
// When browser decoder needs a keyframe (e.g., after packet loss)
pc.OnPLI(func(ti webrtc.TrackID) {
    // Signal encoder to produce IDR frame
    encoder.RequestKeyframe()
})
```

### 7.5 Input DataChannel

```go
func setupInputChannel(pc *webrtc.PeerConnection) (*webrtc.DataChannel, error) {
    dc, err := pc.CreateDataChannel("input", &webrtc.DataChannelInit{
        Ordered:        ptrBool(true),    // Guaranteed order
        MaxRetransmits: ptrUint16(3),     // Limited retransmit for latency
    })
    if err != nil {
        return nil, err
    }

    dc.OnMessage(func(msg webrtc.DataChannelMessage) {
        var event InputEvent
        json.Unmarshal(msg.Data, &event)
        inputInjector.Inject(event)
    })

    return dc, nil
}
```

### 7.6 Control DataChannel

```go
func setupControlChannel(pc *webrtc.PeerConnection) (*webrtc.DataChannel, error) {
    dc, err := pc.CreateDataChannel("control", &webrtc.DataChannelInit{
        Ordered:        ptrBool(true),
        MaxRetransmits: ptrUint16(0),     // Unlimited retransmit — must be reliable
    })

    // Handle: ping/pong, quality change requests, reconnect signals
    dc.OnMessage(func(msg webrtc.DataChannelMessage) {
        var ctrl ControlMessage
        json.Unmarshal(msg.Data, &ctrl)
        handleControlMessage(ctrl)
    })

    return dc, nil
}
```

### 7.7 Signaling Service

```go
// internal/remote/signaling/signaling.go

// WebSocket signaling relay between browser and WebRTC peer
type SignalingRelay struct {
    sessions map[string]*Session
    mu       sync.Mutex
}

// SDP exchange
type SignalMessage struct {
    Type    string `json:"type"`    // "offer", "answer", "ice-candidate"
    SDP     string `json:"sdp,omitempty"`
    ICE     string `json:"ice,omitempty"`
    SID     string `json:"sid"`     // Session ID
}
```

**Flow**:
1. Browser creates SDP offer → sends via WebSocket to 9ed
2. 9ed creates Pion PeerConnection, sets remote description, creates answer
3. Answer sent back via WebSocket
4. ICE candidates exchanged via WebSocket until connection established
5. After ICE: media flows directly browser↔9ed (not through WebSocket)

### 7.8 Latency Optimization Settings

```go
// Pion settings for minimum latency
settingEngine := webrtc.SettingEngine{}

// Disable SRTP replay protection (reduces latency for real-time video)
settingEngine.DisableSRTPReplayProtection(true)

// Smaller jitter buffer
settingEngine.SetJitterBuffer(
    webrtc.JitterBufferSetting{
        MinDelay: 0,  // No minimum delay
    },
)

// ICE configuration
settingEngine.SetICEUDPMux(udpMux)  // Single UDP socket for all connections
```

---

## 8. Adaptive Bitrate Engine

### 8.1 Metrics Collection

```go
type StreamMetrics struct {
    // Network
    RTTMs          float64   // Round-trip time
    PacketLossRate float64   // 0.0 - 1.0
    AvailableBwKbps int64    // Estimated available bandwidth

    // Encoder
    EncodeMs       float64   // Time to encode last frame
    FPS            float64   // Actual output FPS
    BitrateKbps    int64     // Current configured bitrate

    // Capture
    CaptureMs      float64   // Time to capture last frame
    FrameQueueLen  int       // Pending frames in encode queue

    Timestamp      int64
}
```

### 8.2 Adaptation Controller

```go
// internal/remote/stream/controller.go

type AdaptiveController struct {
    config    AdaptiveConfig
    metrics   chan StreamMetrics
    encoder   EncoderControl  // Interface to change encoder params
}

func (c *AdaptiveController) Run(ctx context.Context) {
    ticker := time.NewTicker(500 * time.Millisecond) // Check every 500ms
    for {
        select {
        case <-ticker.C:
            c.adapt()
        case m := <-c.metrics:
            c.updateMetrics(m)
        case <-ctx.Done():
            return
        }
    }
}

func (c *AdaptiveController) adapt() {
    // Decision logic based on metrics:
    //
    // If RTT > 150ms && packetLoss > 2%:
    //   → Reduce bitrate by 20%, reduce FPS to 30
    //
    // If RTT < 50ms && packetLoss < 0.5% && queueLen < 2:
    //   → Increase bitrate by 10% (up to max)
    //
    // If encodeMs > 15ms:
    //   → Reduce resolution (e.g., 1080p → 720p)
    //
    // If frameQueueLen > 5:
    //   → Drop oldest frames, reduce FPS
}
```

### 8.3 Resolution Change Protocol

Resolution changes require encoder reinitialization (NVENC limitation). Protocol:

1. Controller decides resolution change needed
2. Signal encoder to flush current frames
3. Destroy NVENC session
4. Create new NVENC session with new resolution
5. Capture backend adjusts output size
6. Resume streaming

This causes ~50-200ms gap. Only trigger on sustained metric degradation, not transient spikes.

---

## 9. Clipboard Sync (Phase 2)

### 9.1 Architecture

```go
// internal/remote/clipboard/clipboard.go

type ClipboardSync struct {
    localClipboard  ClipboardBackend  // Platform-specific clipboard access
    remoteClipboard *webrtc.DataChannel
    lastContent     ClipboardContent
    mu              sync.Mutex
}

type ClipboardContent struct {
    Type     string `json:"type"`     // "text", "image"
    TextData string `json:"text,omitempty"`
    ImageB64 string `json:"image,omitempty"` // Base64 encoded
    Source   string `json:"source"`   // "local" or "remote"
}
```

**Flow**:
1. Poll local clipboard every 200ms (Windows: `OpenClipboard`/`GetClipboardData`)
2. If changed → send to remote via DataChannel
3. Receive remote clipboard change → set local clipboard
4. Anti-loop: track last content hash, skip if same

---

## 10. Audio Streaming (Phase 2)

### 10.1 Capture (Windows WASAPI)

```go
// internal/remote/audio/wasapi/wasapi.go
/*
#cgo LDFLAGS: -lole32 -lmmdevapi

#include <mmdeviceapi.h>
#include <audioclient.h>

HRESULT wasapi_init(IMMDevice** ppDevice, IAudioClient** ppClient);
HRESULT wasapi_capture(IAudioClient* pClient, float** ppData, UINT32* pFrames);
*/
import "C"
```

### 10.2 Audio Transport

Audio sent via separate WebRTC audio track (Opus codec):

```go
audioTrack, _ := webrtc.NewTrackLocalStaticSample(
    webrtc.RTPCodecCapability{
        MimeType:  webrtc.MimeTypeOpus,
        ClockRate: 48000,
        Channels:  2,
    },
    "audio", "9ed-remote-audio",
)
```

---

## 11. Session Management

### 11.1 Session Lifecycle

```go
// internal/remote/session.go

type RemoteSession struct {
    ID            string
    PeerConn      *webrtc.PeerConnection
    Capture       capture.CaptureBackend
    Encoder       encode.Encoder
    InputInjector input.Injector
    Controller    *AdaptiveController
    CreatedAt     time.Time
    LastActiveAt  time.Time
    State         SessionState  // "connecting", "active", "reconnecting", "closed"
}

type SessionState string

const (
    StateConnecting    SessionState = "connecting"
    StateActive        SessionState = "active"
    StateReconnecting  SessionState = "reconnecting"
    StateClosed        SessionState = "closed"
)
```

### 11.2 Reconnection

```
1. Browser detects ICE connection disconnected
2. Browser sends "reconnect" via control DataChannel (if still open)
   OR creates new WebSocket signaling connection
3. Server looks up session by ID
4. If session exists and not timed out:
   a. Create new PeerConnection (ICE restart)
   b. Re-use existing capture/encode pipeline
   c. Force keyframe on first frame after reconnect
5. If session expired: return error, client shows "session expired"
```

**Session timeout**: Configurable, default 86400s (24h). Active sessions reset timer on each packet.

### 11.3 Concurrent Session Limit

```go
// Check before creating new session
if len(activeSessions) >= cfg.MaxConcurrentSessions {
    return errors.New("max concurrent sessions reached")
}
```

Enforced at session creation. Not a queue — rejects with error.

---

## 12. SQLite Persistence

### 12.1 Schema Extension

```sql
-- Added to existing migrateSchema() in chat/store.go

CREATE TABLE IF NOT EXISTS remote_sessions (
    id TEXT PRIMARY KEY,
    user TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'active',
    resolution_width INTEGER,
    resolution_height INTEGER,
    codec TEXT DEFAULT 'h264',
    avg_fps REAL DEFAULT 0,
    avg_rtt_ms REAL DEFAULT 0,
    avg_bitrate_kbps INTEGER DEFAULT 0,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    ended_at DATETIME,
    duration_seconds INTEGER
);

CREATE TABLE IF NOT EXISTS remote_session_stats (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL REFERENCES remote_sessions(id),
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    fps REAL,
    rtt_ms REAL,
    packet_loss REAL,
    bitrate_kbps INTEGER,
    encode_ms REAL,
    capture_ms REAL
);
```

---

## 13. Frontend Components

### 13.1 RemoteViewer Component

```tsx
// frontend/src/components/remote/RemoteViewer.tsx

interface RemoteViewerProps {
  sessionId: string;
  onDisconnect: () => void;
}

// Key responsibilities:
// 1. Create RTCPeerConnection
// 2. Handle ICE candidates
// 3. Render video stream to canvas
// 4. Capture keyboard/mouse events → send via DataChannel
// 5. Handle fullscreen
// 6. Handle dynamic scaling (fit-to-window)

// Uses:
// - RTCPeerConnection API (native browser)
// - Canvas API for rendering (or <video> element with srcObject)
// - DataChannel for input relay
```

**Rendering strategy**: Use `<video>` element with `srcObject = MediaStream` from WebRTC. Browser handles hardware decoding natively. Canvas only needed for overlay (cursor, stats).

### 13.2 Input Capture

```tsx
// frontend/src/components/remote/RemoteInputCapture.tsx

// Captures keyboard and mouse events from the viewer area
// Normalizes to InputEvent format
// Sends via DataChannel

// Key considerations:
// - Capture all keyboard events (preventDefault to avoid browser shortcuts)
// - Map browser key codes to platform-independent format
// - Handle modifier keys (Ctrl, Alt, Shift, Meta)
// - Relative mouse movement for locked cursor mode
// - Touch input mapping (for future mobile support)
```

### 13.3 RemoteStats Component

```tsx
// Real-time stats overlay (top-right corner of viewer)
// Updated every 500ms via control DataChannel

interface RemoteStatsData {
  fps: number;
  rttMs: number;
  bitrateKbps: number;
  packetLoss: number;
  codec: string;
  resolution: string;
  encodeLatencyMs: number;
}
```

### 13.4 Zustand Store

```ts
// frontend/src/stores/remoteStore.ts

interface RemoteState {
  sessions: RemoteSession[];
  activeSessionId: string | null;
  isConnecting: boolean;
  stats: RemoteStatsData | null;

  // Actions
  createSession: () => Promise<string>;
  disconnectSession: (id: string) => Promise<void>;
  fetchSessions: () => Promise<void>;
}
```

---

## 14. Build System & CGo Strategy

### 14.1 Build Tags

```go
// Platform-specific compilation using Go build tags

// internal/remote/capture/dxgi/dxgi.go
//go:build windows

// internal/remote/capture/x11/x11.go
//go:build linux

// internal/remote/capture/quartz/quartz.go
//go:build darwin

// internal/remote/encode/nvenc/nvenc_windows.go
//go:build windows

// internal/remote/encode/vaapi/vaapi.go
//go:build linux

// internal/remote/encode/videotoolbox/videotoolbox.go
//go:build darwin
```

### 14.2 CGo Dependencies

| Platform | Libraries | Build Impact |
|----------|-----------|-------------|
| Windows | d3d11, dxgi, nvEncodeAPI64, user32, ole32, mmdevapi | Requires Windows SDK + NVIDIA SDK |
| Linux | X11, Xext, Xdamage, Xtst, va, va-drm, pipewire-0.3 | Requires dev packages |
| macOS | CoreGraphics, CoreVideo, VideoToolbox, CoreAudio, ApplicationServices | Requires Xcode |

### 14.3 Build Commands

```bash
# Build for current platform (CGo auto-detected)
go build -o 9ed ./cmd/server

# Cross-compilation NOT possible with CGo
# Must build on target platform or use cross-compilation toolchain

# Docker build for Linux
docker build -t 9ed-remote .

# Windows build requires:
# - MinGW or MSVC (for CGo)
# - Windows SDK
# - NVIDIA Video Codec SDK (for NVENC headers)
```

### 14.4 NVENC Header Distribution

NVENC headers (`nvEncodeAPI.h`) are NOT bundled with Windows SDK. They must be:
1. Downloaded from NVIDIA Video Codec SDK
2. Placed in `internal/remote/encode/nvenc/include/`
3. Git-ignored (license restrictions on redistribution)

This adds a build prerequisite step for Windows + NVENC.

---

## 15. Environment Variables

```env
# Core
REMOTE_DESKTOP=false
REMOTE_DESKTOP_HOST=0.0.0.0
REMOTE_DESKTOP_PORT=9095

# Capture
REMOTE_CAPTURE_BACKEND=auto          # auto, dxgi, x11, wayland, quartz

# Encoding
REMOTE_VIDEO_CODEC=h264              # h264 (h265, av1 future)
REMOTE_VIDEO_FPS=60
REMOTE_VIDEO_BITRATE=12000000
REMOTE_VIDEO_MAX_WIDTH=2560
REMOTE_VIDEO_MAX_HEIGHT=1440
REMOTE_VIDEO_PRESET=p4               # NVENC preset: p1-p7
REMOTE_VIDEO_TUNE=ultralowlatency    # NVENC tune

# GPU Encoding
REMOTE_NVENC_ENABLED=true
REMOTE_QUICKSYNC_ENABLED=true
REMOTE_AMF_ENABLED=true
REMOTE_SOFTWARE_FALLBACK=true         # Fall back to OpenH264 if no GPU

# WebRTC
REMOTE_WEBRTC_UDP_PORT_MIN=40000
REMOTE_WEBRTC_UDP_PORT_MAX=40100

# STUN/TURN
REMOTE_STUN_SERVERS=stun:stun.l.google.com:19302
REMOTE_TURN_ENABLED=false
REMOTE_TURN_URL=
REMOTE_TURN_USERNAME=
REMOTE_TURN_PASSWORD=

# Session
REMOTE_SESSION_TIMEOUT=86400
REMOTE_MAX_CONCURRENT_SESSIONS=5
REMOTE_REQUIRE_AUTH=true

# Audio (Phase 2)
REMOTE_AUDIO_ENABLED=false

# Clipboard (Phase 2)
REMOTE_CLIPBOARD_ENABLED=false
```

Total: 22 variables (trimmed from PRD's 30+ by removing redundant/deferred ones).

---

## 16. New Backend Package Layout

```
internal/remote/
  ├── config.go                    — Config struct + env loading
  ├── session.go                   — Session lifecycle management
  ├── signaling.go                 — WebSocket SDP/ICE relay
  ├── api.go                       — REST API handlers
  ├── store.go                     — SQLite session persistence
  │
  ├── capture/
  │   ├── types.go                 — Frame, CaptureBackend interface
  │   ├── dxgi/                    — Windows DXGI capture (build tag: windows)
  │   │   └── dxgi.go
  │   ├── x11/                     — Linux X11 capture (build tag: linux)
  │   │   └── x11.go
  │   ├── wayland/                 — Linux Wayland/PipeWire capture (build tag: linux)
  │   │   └── wayland.go
  │   └── quartz/                  — macOS capture (build tag: darwin)
  │       └── quartz.go
  │
  ├── encode/
  │   ├── types.go                 — EncodedFrame, Encoder interface
  │   ├── nvenc/                   — NVIDIA NVENC (build tags: windows, linux)
  │   │   ├── nvenc.go
  │   │   └── include/
  │   │       └── nvEncodeAPI.h    — (gitignored, manual download)
  │   ├── quicksync/               — Intel QuickSync (build tag: windows)
  │   │   └── quicksync.go
  │   ├── vaapi/                   — VAAPI (build tag: linux)
  │   │   └── vaapi.go
  │   ├── videotoolbox/            — VideoToolbox (build tag: darwin)
  │   │   └── videotoolbox.go
  │   └── openh264/                — Software fallback
  │       └── openh264.go
  │
  ├── input/
  │   ├── types.go                 — InputEvent, Injector interface
  │   ├── windows/                 — SendInput (build tag: windows)
  │   │   └── input.go
  │   ├── x11/                     — XTest (build tag: linux)
  │   │   └── input.go
  │   └── quartz/                  — CGEvent (build tag: darwin)
  │       └── input.go
  │
  ├── audio/                       — Phase 2
  │   ├── wasapi/                  — Windows WASAPI
  │   ├── pulseaudio/              — Linux PulseAudio
  │   └── coreaudio/               — macOS CoreAudio
  │
  ├── clipboard/                   — Phase 2
  │   ├── clipboard.go
  │   ├── windows.go
  │   ├── linux.go
  │   └── darwin.go
  │
  └── stream/
      ├── controller.go            — Adaptive bitrate controller
      ├── metrics.go               — Metrics collection/aggregation
      └── pipeline.go              — Orchestrates capture→encode→transport
```

---

## 17. Dependencies Added to go.mod

```
# WebRTC
github.com/pion/webrtc/v3          — WebRTC implementation
github.com/pion/rtp                 — RTP packetization
github.com/pion/interceptor         — Bandwidth estimation (GCC)

# Codecs
github.com/pion/codecs             — H264 depayloader/parser

# Utilities
github.com/pion/udp                 — UDP mux for WebRTC
github.com/pion/stun                — STUN client
github.com/pion/turn                — TURN client (if TURN enabled)
```

NVENC, DXGI, etc. are system libraries (CGo linked), not Go modules.

---

## 18. API Routes

```
# Session management
POST   /api/remote/sessions              — Create remote session
GET    /api/remote/sessions              — List active sessions
GET    /api/remote/sessions/:id          — Get session details
DELETE /api/remote/sessions/:id          — Disconnect + cleanup
POST   /api/remote/sessions/:id/reconnect — Reconnect to existing session

# Stats
GET    /api/remote/sessions/:id/stats    — Current session stats
GET    /api/remote/sessions/:id/history  — Historical stats

# Capabilities
GET    /api/remote/capabilities          — What this server supports (encoders, capture, etc)

# Signaling
WS     /ws/remote/signaling/:id          — SDP/ICE relay

# Admin
GET    /api/remote/diagnostics           — System diagnostics (Phase 2)
```

---

## 19. Security Model

### 19.1 Auth Integration

Remote desktop inherits 9ed's Basic Auth. All REST API + WebSocket endpoints protected by existing auth middleware.

### 19.2 DTLS-SRTP

WebRTC media encrypted by default via DTLS-SRTP. No additional encryption layer needed.

### 19.3 Input Validation

```go
// Rate limit input events to prevent abuse
const maxInputRateHz = 120 // Max 120 events/second

// Sanitize input events
func validateInput(e InputEvent) error {
    if e.X < 0 || e.X > 7680 { return errInvalidCoords }
    if e.Y < 0 || e.Y > 4320 { return errInvalidCoords }
    // etc.
}
```

### 19.4 Session Isolation

- Each session gets unique PeerConnection
- Sessions cannot access each other's data
- Input injection scoped to active session only
- Session timeout enforced server-side

---

## 20. Performance Budget

### 20.1 Latency Breakdown (Target — LAN)

| Stage | Target | Notes |
|-------|--------|-------|
| DXGI Capture | 2ms | AcquireNextFrame + staging copy |
| Frame Transfer (Go channel) | <0.1ms | In-process |
| NVENC Encode | 3ms | preset=p1, tune=ultralowlatency |
| RTP Packetization (Pion) | <0.5ms | In-process |
| Network (LAN) | 1-5ms | Switched Ethernet |
| WebRTC jitter buffer | 5-20ms | Browser-controlled |
| H264 Decode (browser HW) | 2-5ms | GPU hardware decode |
| Canvas Render | <1ms | Browser compositor |
| **Total** | **14-36ms** | **Achievable on LAN** |

### 20.2 Latency Breakdown (Target — WAN)

| Stage | Target | Notes |
|-------|--------|-------|
| Capture + Encode | 5ms | Same as LAN |
| Network (WAN) | 20-60ms | Internet-dependent |
| WebRTC jitter buffer | 20-40ms | Browser compensates for jitter |
| Decode + Render | 5ms | Same as LAN |
| **Total** | **50-110ms** | **Network-dependent** |

### 20.3 CPU & Memory Budget

| Resource | Budget | Notes |
|----------|--------|-------|
| CPU (capture) | <5% | DXGI is GPU-efficient |
| CPU (encode) | <3% | NVENC offloads to GPU |
| CPU (Pion) | <5% | RTP packetization |
| Memory | <100MB | Frame buffers + encode queue |
| GPU | Shared | Uses existing display GPU |
| Bandwidth | 5-15 Mbps | H264 at 1080p60 |

---

## 21. Testing Strategy

### 21.1 Unit Tests

```go
// Encoder interface tests (mock backend)
func TestEncoderProducesNALUnits(t *testing.T) { ... }
func TestEncoderKeyframeRequest(t *testing.T) { ... }

// Adaptive controller tests
func TestBitrateReductionOnPacketLoss(t *testing.T) { ... }
func TestResolutionReductionOnHighEncodeTime(t *testing.T) { ... }

// Input validation tests
func TestInputEventValidation(t *testing.T) { ... }

// Session lifecycle tests
func TestSessionCreateAndCleanup(t *testing.T) { ... }
func TestConcurrentSessionLimit(t *testing.T) { ... }
```

### 21.2 Integration Tests

```go
// Signaling flow test (with mock Pion peer)
func TestSignalingSDPExchange(t *testing.T) { ... }

// Capture → Encode pipeline test (with synthetic frames)
func TestCaptureEncodePipeline(t *testing.T) { ... }
```

### 21.3 Platform Tests

```go
// Only run on Windows with DXGI
//go:build windows && dxgi

func TestDXGICapture(t *testing.T) {
    if os.Getenv("TEST_REMOTE_DESKTOP") == "" {
        t.Skip("Set TEST_REMOTE_DESKTOP=1 to run capture tests")
    }
    // ... actual DXGI capture test
}
```

---

## 22. Phase Breakdown & Timeline

### Phase 1 — Core Pipeline (12-16 weeks)

| Week | Deliverable |
|------|-------------|
| 1-2 | Config, session types, signaling service, WebSocket relay |
| 3-4 | DXGI capture CGo wrapper, frame pipeline (capture → channel) |
| 5-7 | NVENC encode CGo wrapper, encode pipeline |
| 8-9 | Pion WebRTC integration, video track, NAL injection |
| 10 | Input DataChannel, Windows input injection (SendInput) |
| 11 | Frontend: RemoteViewer, input capture, WebRTC peer |
| 12 | Session management, REST API, sidebar integration |
| 13 | Adaptive bitrate controller (basic) |
| 14 | Testing, bug fixes, performance tuning |
| 15-16 | Polish, documentation, edge cases |

**Phase 1 delivers**: Windows remote desktop with H264/NVENC, keyboard/mouse, browser client.

### Phase 2 — Cross-Platform + Audio (8-12 weeks)

| Week | Deliverable |
|------|-------------|
| 1-3 | Linux X11/Wayland capture |
| 4-5 | VAAPI encode + NVENC Linux |
| 6-7 | X11 input injection |
| 8-9 | WASAPI/PulseAudio audio capture + Opus transport |
| 10-11 | Clipboard sync (text + image) |
| 12 | Testing + polish |

### Phase 3 — Optimization (6-8 weeks)

| Week | Deliverable |
|------|-------------|
| 1-2 | macOS capture + VideoToolbox |
| 3-4 | Dirty region encoding optimization |
| 5-6 | Adaptive bitrate v2 (bandwidth estimation) |
| 7-8 | Session recording, MP4 export |

### Phase 4 — Advanced (8-12 weeks)

| Week | Deliverable |
|------|-------------|
| 1-3 | AV1 codec support |
| 4-6 | QUIC transport (quic-go) |
| 7-8 | Multi-monitor optimization |
| 9-12 | Mobile optimization, touch input |

---

## 23. Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| CGo crashes in capture/encode | Medium | High | Run capture in separate goroutine with recover(); consider separate process |
| NVENC driver incompatibility | Medium | High | Software fallback (OpenH264); detect GPU capabilities at startup |
| Build complexity across 3 OS | High | Medium | CI matrix builds; Docker for Linux; clear build docs |
| Pion H264 quality/bitrate control | Low | Medium | Direct NAL injection gives full control; tune encoder params |
| Browser WebRTC compatibility | Low | Medium | Test Chrome/Edge/Firefox/Safari; graceful degradation messages |
| XSS/input injection abuse | Medium | Critical | Rate limit input; validate all events; auth required |
| Memory leaks in CGo boundary | Medium | Medium | Careful C memory management; go vet + ASAN testing |
| NVENC session limit exceeded | Low | Medium | Check at session creation; queue or reject with clear message |
| Cross-compilation impossible | High | Medium | Accept: build on target platform; provide Dockerfile/VM images |
| Dirty region encoding bugs | Medium | Low | Phase 3 feature; start with full-frame; dirty rects as optimization |

---

## 24. Key Differences from Approach A (Sunshine)

| Aspect | Approach A (Sunshine) | Approach C (From Scratch) |
|--------|----------------------|---------------------------|
| Binary count | 2 (9ed + Sunshine) | 1 (9ed only) |
| CGo in 9ed | None | Heavy (capture + encode + input) |
| Build complexity | Low | High (platform-specific CGo) |
| Latency control | Medium (Sunshine mediates) | Full (every stage tunable) |
| Feature velocity | Limited by Sunshine | Full control |
| Maintenance | Low (Sunshine maintained) | High (3 OS capture pipelines) |
| Time to MVP | 6-8 weeks | 12-16 weeks |
| Long-term potential | Medium (bounded by Sunshine) | High (unlimited customization) |
| Offline capability | Requires Sunshine installed | Self-contained |

---

## 25. Decision Criteria

Choose Approach C (from scratch) if:
- ✅ You need single-binary deployment
- ✅ You want full control over every millisecond of latency
- ✅ You're willing to maintain 3 OS capture pipelines
- ✅ You accept 12-16 week MVP timeline
- ✅ You have or will build CGo expertise

Choose Approach A (Sunshine) if:
- ✅ You want fastest MVP (6-8 weeks)
- ✅ You want to keep 9ed pure Go
- ✅ You can accept external process dependency
- ✅ You want production-quality streaming from day 1
