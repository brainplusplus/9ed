# 9ed Remote Desktop Module — Design Document

**Date**: 2026-05-13
**Status**: Draft — Awaiting design approval
**Author**: Xi Jinping (analysis & design)

---

## 1. Executive Summary

This document critically evaluates the PRD for 9ed Remote Desktop and proposes a recommended approach. The PRD is ambitious and well-visioned, but the scope is 5-10x larger than surface-level estimation. Three approaches are presented with the recommendation being **Approach A: Sunshine Integration**.

---

## 2. Critical PRD Analysis

### 2.1 Scope Assessment

The PRD defines **15 major features** for a module to be built by what appears to be a small team. For context:

- **RustDesk** took years with a dedicated team to reach comparable functionality
- **Parsec** has dozens of engineers focused solely on video streaming
- **Phase 1 MVP alone** (DXGI + NVENC + WebRTC + input + clipboard) is 3-6 months for 1 senior engineer

### 2.2 Critical Issues Identified

#### Issue #1: CGo Dependency Explosion

Current 9ed is **pure Go, zero CGo**. Remote desktop with native capture forces:

| Component | C Dependency | Impact |
|-----------|-------------|--------|
| DXGI Desktop Duplication | `d3d11.h`, `dxgi.h` | CGo mandatory |
| NVENC encoding | `nvEncodeAPI.h` | CGo mandatory |
| WASAPI audio | COM APIs | CGo mandatory |
| X11 capture | `Xlib.h` | CGo mandatory |
| PipeWire | C bindings | CGo mandatory |

This contradicts the "single binary deployment" goal and makes cross-compilation significantly harder.

#### Issue #2: Agent vs In-Process Architecture Undecided

The PRD mentions "Multi OS Agent" but doesn't specify whether capture/encoding runs:
- **In-process**: Simpler deployment, but capture crash = entire 9ed crash. Same-machine only.
- **Separate agent**: Robust, but contradicts "single binary". More deployment complexity.

This is a foundational architecture decision that affects everything downstream.

#### Issue #3: WebRTC Signaling Underspecified

Missing details:
- SDP exchange format and signaling protocol
- ICE candidate trickling strategy
- WebRTC peer connection relationship to existing WebSocket auth
- Symmetric NAT handling strategy
- TURN relay deployment model (who runs it?)

#### Issue #4: GPU Encoding Pipeline Complexity Underestimated

Real-world concerns not addressed:
- **NVENC session limit**: Consumer GPUs limited to 3 concurrent sessions
- **Encoder cold start**: 50-200ms initialization
- **Zero-copy aspiration vs reality**: Pion WebRTC doesn't support direct GPU memory input. NAL unit extraction goes through CPU RAM.
- **Rate control strategy**: CBR vs VBR vs CQP not specified
- **Keyframe interval**: IDR frame frequency for seek/reconnect not specified

**Latency targets are optimistic**: "Encode <10ms" is achievable for NVENC alone. Full pipeline (capture + encode + packetize + network + decode + render) realistically 30-80ms on LAN.

#### Issue #5: Browser Hardware Decode Fragmentation

| Codec | Chrome | Firefox | Safari | Notes |
|-------|--------|---------|--------|-------|
| H264 | Yes | Yes | Yes | Safe cross-browser choice |
| H265/HEVC | Limited | No | Yes | Licensing nightmare |
| AV1 | 96+ | 125+ | 17+ | Hardware decode limited to new GPUs |
| WebCodecs API | 94+ | No | No | Not cross-browser |

"Browser first" differentiator is limited by browser capabilities. H264 Baseline is the only safe cross-browser choice.

#### Issue #6: Latency Targets Treat Network as Software Problem

| Target | Reality |
|--------|---------|
| LAN 20-50ms | Ambitious — Parsec achieves ~15ms LAN with years of optimization |
| WAN 50-120ms | Depends on network path, not code |
| Total E2E <80ms WAN | Unlikely without aggressive optimization AND favorable network |

#### Issue #7: 30+ New Environment Variables

Current 9ed has ~8 env vars. PRD proposes 30+ new ones. Consider TOML/YAML config file for complex nested configuration.

#### Issue #8: Integration Value Unclear

The "developer native" positioning needs a clearer answer to: **Why would a developer use this instead of SSH + VS Code Remote?** The differentiator only matters for GUI workstations where the user needs visual access.

---

## 3. Three Approaches

### Approach A: Sunshine Integration (Recommended)

**Strategy**: Integrate Sunshine (open-source game streaming server) as the capture/encode agent. Build only the 9ed signaling + UI layer.

**Architecture**:
```
Browser (9ed React)
  ↕ WebRTC media stream (direct after signaling)
9ed Go Server (signaling relay + session mgmt + auth)
  ↕ HTTP API (localhost)
Sunshine Process (DXGI + NVENC + WebRTC server)
```

| Aspect | Detail |
|--------|--------|
| MVP Time | 6-8 weeks (1 engineer) |
| LAN Latency | ~30ms (production-grade from day 1) |
| CGo Required | No — 9ed stays pure Go |
| GPU Features | Free (NVENC/QuickSync/AMF via Sunshine) |
| Maintainability | High — follows existing tunnel subprocess pattern |
| Control | Medium — limited to Sunshine's capabilities |
| macOS Support | Separate solution needed (Phase 2) |

**What you build**: Signaling service, session manager, agent subprocess lifecycle, frontend RemoteViewer/Toolbar/Stats, sidebar integration.

**What you DON'T build**: DXGI capture, NVENC pipeline, adaptive bitrate engine, input injection.

### Approach B: Guacamole-Based Lightweight Client

**Strategy**: Use Apache Guacamole as remote desktop gateway.

| Aspect | Detail |
|--------|--------|
| MVP Time | 4-6 weeks |
| LAN Latency | ~80-150ms (RDP/VNC overhead) |
| Protocols | RDP, VNC, SSH |
| GPU Features | None |
| Trade-off | Betrays "ultra low latency" core promise |

**Assessment**: Better as a **complement** (for headless servers) than the primary solution.

### Approach C: Build From Scratch (Original PRD)

**Strategy**: Implement everything — DXGI, NVENC, signaling — from scratch in Go.

| Aspect | Detail |
|--------|--------|
| MVP Time | 6-12 months (1 engineer) |
| LAN Latency | ~25-50ms (optimistic) |
| CGo Required | Heavy — DXGI, NVENC, WASAPI, X11, PipeWire |
| Risk | Very high — massive surface area for 1 person |
| Control | Full |

**Assessment**: High risk for single-engineer team. Quality uncertain.

### Comparison Matrix

| | A: Sunshine | B: Guacamole | C: From Scratch |
|---|---|---|---|
| MVP Time | 6-8 weeks | 4-6 weeks | 6-12 months |
| LAN Latency | ~30ms | ~80-150ms | ~25-50ms |
| CGo Required | No | No | Heavy |
| Maintainability | High | High | Low |
| GPU Features | Free | None | Must build |
| Control Level | Medium | Low | Full |
| macOS Support | Separate | Via VNC | Must build |
| Binary Deployment | 9ed + Sunshine | 9ed + guacd + Java | Single (ideal) |

---

## 4. Recommended Design (Approach A — Expanded)

### 4.1 Architecture Diagram

```
┌─────────────────────────────────────────────────────┐
│  Browser (9ed React)                                │
│  RemoteViewer (Canvas + WebRTC)                     │
│  RemoteToolbar (quality, fullscreen, reconnect)     │
│  RemoteStats (FPS, RTT, bitrate)                    │
└──────────────────────┬──────────────────────────────┘
                       │ WebRTC (ICE/DTLS)
┌──────────────────────┼──────────────────────────────┐
│  9ed Go Server       │                              │
│  ├── Signaling Svc   │  /ws/remote/signaling        │
│  ├── Session Manager │  /api/remote/sessions        │
│  ├── Agent Manager   │  Sunshine subprocess lifecycle│
│  └── Config          │  REMOTE_DESKTOP=true         │
└──────────────────────┼──────────────────────────────┘
                       │ HTTP API (localhost:47990)
┌──────────────────────┼──────────────────────────────┐
│  Sunshine Process (auto-installed, managed)          │
│  ├── DXGI Capture    │  Screen capture (Windows)    │
│  ├── NVENC/QuickSync │  GPU hardware encoding       │
│  ├── WebRTC Server   │  Media streaming             │
│  └── Input Handler   │  Keyboard/mouse injection    │
└─────────────────────────────────────────────────────┘
```

### 4.2 New Backend Packages

```
internal/
  remote/
    config.go        — RemoteDesktop config struct + validation
    signaling.go     — WebSocket signaling relay (SDP/ICE)
    session.go       — Remote session lifecycle management
    agent.go         — Sunshine subprocess manager (start/stop/health)
    api.go           — REST API handlers for /api/remote/*
    store.go         — Session persistence (extend existing SQLite)
```

### 4.3 New API Routes

```
POST   /api/remote/sessions         — Create remote session
GET    /api/remote/sessions         — List active sessions
DELETE /api/remote/sessions/:id     — Disconnect session
GET    /api/remote/sessions/:id/stats — Get streaming stats
GET    /api/remote/agent/status     — Sunshine agent health
POST   /api/remote/agent/install    — Trigger Sunshine install
WS     /ws/remote/signaling/:id     — WebRTC signaling relay
```

### 4.4 Environment Variables (Trimmed from 30+ to 8)

```env
REMOTE_DESKTOP=false
REMOTE_DESKTOP_PORT=47990
REMOTE_VIDEO_FPS=60
REMOTE_VIDEO_BITRATE=12000000
REMOTE_STUN_SERVERS=stun:stun.l.google.com:19302
REMOTE_TURN_ENABLED=false
REMOTE_TURN_URL=
REMOTE_TURN_CREDENTIALS=
```

Complex Sunshine configuration (codec, resolution, GPU selection) is handled through Sunshine's own config file, generated by 9ed.

### 4.5 New Frontend Components

```
frontend/src/
  components/remote/
    RemoteViewer.tsx       — Canvas + WebRTC peer connection
    RemoteToolbar.tsx      — Fullscreen, quality, reconnect
    RemoteStats.tsx        — FPS, RTT, bitrate display
    RemoteSessionList.tsx  — Active sessions panel
  stores/
    remoteStore.ts         — Zustand store for remote state
  hooks/
    useWebRTC.ts           — WebRTC connection management
    useRemoteSession.ts    — Session lifecycle
```

### 4.6 Signaling Flow

```
Browser                 9ed Server              Sunshine
   │                        │                       │
   │ POST /api/remote/      │                       │
   │ sessions               │  Start Sunshine       │
   │───────────────────────►│──────────────────────►│
   │ {sessionId, wsUrl}     │                       │
   │◄───────────────────────│                       │
   │                        │                       │
   │ WS /ws/remote/         │  Pair request         │
   │ signaling/:id          │──────────────────────►│
   │═══════════════════════►│                       │
   │                        │  PIN                  │
   │ {type:"pin",pin:"X"}   │◄──────────────────────│
   │◄───────────────────────│                       │
   │                        │                       │
   │ SDP Offer              │  SDP Offer            │
   │───────────────────────►│──────────────────────►│
   │ SDP Answer             │  SDP Answer           │
   │◄───────────────────────│◄──────────────────────│
   │ ICE Candidates         │  ICE Candidates       │
   │◄══════════════════════►│◄═════════════════════►│
   │                        │                       │
   │ ═══ Direct WebRTC Media Stream ═══             │
   │◄═══════════════════════════════════════════════►│
```

**Key insight**: After ICE negotiation, media flows **directly between browser and Sunshine**. 9ed handles signaling relay only, not video data.

### 4.7 Module Integration Pattern

Follows existing 9ed patterns exactly:

- **Config gating**: `cfg.Mode == "full" && cfg.RemoteDesktop` (same as chat, watcher)
- **Subprocess lifecycle**: Same pattern as `internal/tunnel/`
- **REST + WebSocket**: Same pattern as `internal/httpapi/`
- **Zustand store**: Same pattern as `stores/gitStore.ts`, `stores/chatStore.ts`
- **Sidebar entry**: Same pattern as git/chat activity bar entries

### 4.8 MVP Scope (Phase 1)

**IN scope**:
- Screen streaming (video only, no audio)
- Keyboard input
- Mouse input
- Session create/list/disconnect
- Fullscreen mode
- Basic stats (FPS, RTT)
- Windows support (via Sunshine + DXGI)
- Auto-install Sunshine binary

**NOT in scope** (deferred):
- Audio streaming (Phase 2)
- Clipboard sync (Phase 2)
- File transfer (use existing 9ed file ops)
- Recording (Phase 2)
- Codec selection UI (Sunshine handles)
- Diagnostics UI (Phase 2)
- macOS/Linux support (Phase 2)
- E2EE (Phase 3)
- Native client (Phase 4)

### 4.9 Evolution Path

| Phase | Focus | Timeline |
|-------|-------|----------|
| Phase 1 | Sunshine integration, Windows, browser-only, KB/mouse | 6-8 weeks |
| Phase 2 | Linux support, audio, clipboard, auto-install improvements | 4-6 weeks |
| Phase 3 | Replace Sunshine with custom capture agent (if needed), dirty regions | 8-12 weeks |
| Phase 4 | AV1, QUIC transport, macOS agent | 8-12 weeks |

### 4.10 Effort Estimate

| Component | Estimate |
|-----------|----------|
| Backend config + validation | 2 days |
| Agent lifecycle manager (Sunshine wrapper) | 3 days |
| Signaling service (WebSocket relay) | 3 days |
| Session manager + REST API | 2 days |
| Frontend RemoteViewer (WebRTC) | 5 days |
| RemoteToolbar + RemoteStats | 2 days |
| remoteStore (Zustand) | 1 day |
| Sidebar entry + routing | 1 day |
| Sunshine auto-install | 2 days |
| Testing + bug fixes | 5 days |
| **Total** | **~26 days / 5-6 weeks** |

---

## 5. Risks

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Sunshine API changes | Low | Medium | Pin Sunshine version, abstract behind interface |
| Sunshine not installed / incompatible | Medium | High | Auto-install + clear error messages |
| WebRTC NAT traversal failures | Medium | High | TURN relay fallback, diagnostics UI |
| NVENC session limit hit | Low | Medium | Document limits, queue sessions |
| Browser WebRTC compatibility | Low | Medium | Feature detection, graceful fallback message |
| 9ed user doesn't have GPU | Low | Low | Sunshine supports software encoding as fallback |

---

## 6. Open Questions (Need User Input)

1. **Team size & timeline**: How many engineers? What's the target date?
2. **Primary use case**: GUI workstation access? Or also headless servers?
3. **Agent architecture preference**: Separate process (recommended) or embedded?
4. **Windows-first confirmation**: Ready for complex build pipeline implications?
5. **MVP tolerance**: Is "streaming + input only, no audio/file transfer/recording" acceptable for v1?
6. **Approach selection**: A (Sunshine), B (Guacamole), or C (from scratch)?

---

## 7. Decision Required

This design document presents the analysis and recommendation. **User must approve an approach before implementation planning begins.**

Recommended next step: User selects approach → Write implementation plan → Execute.
