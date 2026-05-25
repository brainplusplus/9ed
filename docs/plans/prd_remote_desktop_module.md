# PRD — 9ed Remote Desktop Module

## Product Name

9ed Remote Desktop

## Overview

9ed Remote Desktop adalah modul tambahan pada platform 9ed yang memungkinkan user mengakses desktop/workstation remote secara ultra low latency melalui browser maupun native client.

Target utama:

* Developer workstation
* AI workstation
* GPU workstation
* Remote coding environment
* Multi OS remote access
* Browser-first remote desktop
* Self-hosted friendly

Fokus utama modul ini adalah:

* Ultra low latency
* Lightweight architecture
* Multi-channel protocol
* Developer-oriented remote workflow
* Hybrid remote architecture

---

# Goals

## Primary Goals

### 1. Ultra Low Latency Remote Desktop

Target latency:

* LAN: 20ms–50ms
* WAN: 50ms–120ms

### 2. Browser Accessible

Remote desktop dapat diakses langsung dari browser tanpa install client tambahan.

### 3. Multi OS Support

Support:

* Windows
* Linux
* macOS

### 4. GPU Accelerated Streaming

Support:

* NVIDIA NVENC
* Intel QuickSync
* AMD AMF

### 5. Seamless Integration With 9ed

Remote desktop menjadi bagian native dari ecosystem 9ed.

### 6. Self Hosted Friendly

Semua fitur dapat berjalan:

* local server
* VPS
* dedicated server
* baremetal
* Kubernetes

---

# Non Goals

## Not Initial Priority

### 1. Gaming Streaming

Walaupun capable, fokus awal bukan gaming.

### 2. Mobile Native Client

Initial release fokus browser dan desktop browser.

### 3. Multi Monitor Optimization

Support ada, tapi advanced optimization deferred.

### 4. Audio Production Grade Latency

Audio support ada, tapi bukan fokus utama.

---

# Environment Variables

## Core Configuration

```env
REMOTE_DESKTOP=false
```

Default:

```env
REMOTE_DESKTOP=false
```

Jika:

```env
REMOTE_DESKTOP=true
```

Maka:

* Sidebar menu Remote muncul
* Remote services aktif
* Signaling server aktif
* Remote API aktif
* WebRTC transport aktif

---

# Additional Environment Variables

## Remote Desktop Core

```env
REMOTE_DESKTOP=true
REMOTE_DESKTOP_HOST=0.0.0.0
REMOTE_DESKTOP_PORT=9095
REMOTE_DESKTOP_PUBLIC_URL=https://remote.example.com
```

---

## WebRTC

```env
REMOTE_WEBRTC_ENABLED=true
REMOTE_WEBRTC_UDP_PORT_MIN=40000
REMOTE_WEBRTC_UDP_PORT_MAX=40100
REMOTE_WEBRTC_FORCE_RELAY=false
```

---

## STUN/TURN

```env
REMOTE_STUN_SERVERS=stun:stun.l.google.com:19302
REMOTE_TURN_ENABLED=false
REMOTE_TURN_HOST=
REMOTE_TURN_PORT=3478
REMOTE_TURN_USERNAME=
REMOTE_TURN_PASSWORD=
```

---

## Video Streaming

```env
REMOTE_VIDEO_CODEC=h264
REMOTE_VIDEO_FPS=60
REMOTE_VIDEO_BITRATE=12000000
REMOTE_VIDEO_MAX_WIDTH=2560
REMOTE_VIDEO_MAX_HEIGHT=1440
REMOTE_VIDEO_ENABLE_AV1=false
REMOTE_VIDEO_ENABLE_H265=false
```

---

## GPU Encode

```env
REMOTE_NVENC_ENABLED=true
REMOTE_QUICKSYNC_ENABLED=true
REMOTE_AMF_ENABLED=true
```

---

## Security

```env
REMOTE_REQUIRE_AUTH=true
REMOTE_ENABLE_E2EE=false
REMOTE_SESSION_TIMEOUT=86400
REMOTE_MAX_CONCURRENT_SESSION=5
```

---

## Capture

```env
REMOTE_CAPTURE_BACKEND=auto
```

Possible values:

* auto
* dxgi
* x11
* wayland
* quartz

---

# Sidebar UI

## Sidebar Menu

Menu baru:

```text
Remote
```

Icon:

* Monitor
* Desktop
* Screen Share

---

# Remote Menu Structure

```text
Remote
 ├── Sessions
 ├── Devices
 ├── Connections
 ├── Recording
 ├── Clipboard
 ├── File Transfer
 ├── Settings
 └── Diagnostics
```

---

# Main Features

# 1. Remote Desktop Streaming

## Description

User dapat mengakses desktop remote realtime.

---

## Features

### Screen Streaming

* Ultra low latency
* GPU accelerated
* Adaptive bitrate
* Dynamic FPS
* Dirty region updates

---

## Supported Codec

### Initial

* H264

### Future

* H265
* AV1

---

## Adaptive Streaming

Dynamic adjustment:

* FPS
* bitrate
* quality
* resolution

Based on:

* network latency
* packet loss
* RTT
* available bandwidth

---

# 2. Multi OS Agent

## Windows Agent

### Technologies

* DXGI Desktop Duplication
* NVENC
* WASAPI
* Win32 input injection

---

## Linux Agent

### Technologies

* PipeWire
* X11 capture
* Wayland capture
* VAAPI
* PulseAudio

---

## macOS Agent

### Technologies

* ScreenCaptureKit
* VideoToolbox
* CoreAudio
* Quartz Event Services

---

# 3. Browser Client

## Requirements

Compatible browser:

* Chrome
* Edge
* Firefox
* Safari

---

## Browser Features

### Remote Canvas

* WebGL rendering
* hardware decode
* fullscreen
* dynamic scaling

---

### Input

* mouse
* keyboard
* clipboard
* touch input

---

### Multi Tab

Support:

* multiple remote sessions
* reconnect
* session persistence

---

# 4. Native Desktop Client (Future)

## Platforms

* Windows
* Linux
* macOS

---

## Features

* native decode
* hardware decode
* direct keyboard capture
* multi monitor support
* USB passthrough (future)

---

# 5. Ultra Low Latency Architecture

# Transport Layer

## Primary

WebRTC UDP

---

## Fallback

WebSocket TCP

---

## Future

Custom QUIC transport

---

# Multi Channel Architecture

## Separate Channels

### Video Channel

Purpose:

* video stream

Protocol:

* WebRTC media stream

Priority:

* medium

---

### Input Channel

Purpose:

* keyboard
* mouse

Priority:

* highest

Reliable:

* yes

---

### Clipboard Channel

Purpose:

* clipboard sync

Reliable:

* yes

---

### File Transfer Channel

Purpose:

* upload/download

Reliable:

* yes

---

### Control Channel

Purpose:

* session metadata
* ping
* quality adjustment
* reconnect

---

# 6. Dirty Region Streaming

## Description

Hanya area layar yang berubah yang dikirim.

---

## Benefits

* lower bandwidth
* lower latency
* lower CPU usage
* smoother interaction

---

# 7. Hardware Acceleration

## Encode Pipeline

Preferred:

```text
GPU Framebuffer
→ Hardware Encoder
→ WebRTC
→ Hardware Decoder
→ GPU Render
```

Avoid:

```text
GPU
→ CPU RAM
→ Encode
→ CPU Decode
→ GPU
```

---

# 8. Audio Streaming

## Features

* desktop audio
* microphone passthrough
* mute control

---

## Technologies

### Windows

* WASAPI

### Linux

* PulseAudio
* PipeWire

### macOS

* CoreAudio

---

# 9. Clipboard Sync

## Features

* text sync
* image sync
* bi-directional

---

# 10. File Transfer

## Features

* drag and drop
* upload/download
* resume support
* progress tracking

---

# 11. Session Management

## Features

* reconnect session
* persistent session
* session locking
* session timeout
* force disconnect

---

# 12. Authentication

## Initial

9ed auth integration

---

## Future

* SSO
* OAuth
* LDAP
* SAML

---

# 13. Permissions

## Access Control

### Permission Examples

```text
remote.view
remote.control
remote.file_transfer
remote.clipboard
remote.recording
remote.admin
```

---

# 14. Recording

## Features

* session recording
* mp4 export
* screenshots

---

# 15. Monitoring & Diagnostics

## Metrics

* FPS
* RTT
* packet loss
* bandwidth
* codec
* encoder type
* decode latency

---

## Diagnostics Page

### Realtime Stats

* current bitrate
* frame drops
* jitter
* network quality

---

# Backend Architecture

# Services

## 1. Signaling Service

### Responsibilities

* WebRTC negotiation
* ICE exchange
* session setup

---

## 2. Session Manager

### Responsibilities

* session lifecycle
* reconnect
* auth
* permissions

---

## 3. Stream Manager

### Responsibilities

* encoder control
* bitrate adaptation
* FPS control
* codec selection

---

## 4. Agent Service

### Responsibilities

* screen capture
* input injection
* clipboard sync
* audio capture

---

## 5. Diagnostics Service

### Responsibilities

* metrics
* logs
* tracing
* performance analytics

---

# Recommended Technology Stack

# Backend

## Language

Go

---

## Web Framework

Possible:

* Fiber
* Gin
* Echo
* net/http

---

## WebRTC

Pion WebRTC

---

## QUIC

quic-go

---

## Realtime Messaging

Possible:

* WebSocket
* WebRTC DataChannel

---

# Agent Technologies

## Windows

### Capture

* DXGI Desktop Duplication

### Encode

* NVENC
* QuickSync
* AMF

### Input Injection

* SendInput

---

## Linux

### Capture

* PipeWire
* X11
* Wayland

### Encode

* VAAPI
* NVENC

---

## macOS

### Capture

* ScreenCaptureKit

### Encode

* VideoToolbox

---

# Frontend Architecture

# Frontend Stack

## Existing 9ed Stack

* React
* Monaco
* Tailwind

---

## Additional Technologies

* WebRTC API
* WebCodecs
* Canvas/WebGL

---

# Remote Desktop UI

## Main Components

### RemoteViewer

Responsibilities:

* video rendering
* scaling
* fullscreen
* keyboard capture

---

### RemoteToolbar

Features:

* fullscreen
* clipboard
* reconnect
* quality selection
* monitor selection

---

### RemoteStats

Features:

* FPS
* ping
* bitrate
* packet loss

---

# Performance Targets

# Streaming Performance

## Target FPS

* 60 FPS default
* 120 FPS future

---

## Resolution

* 1080p default
* 1440p recommended
* 4K optional

---

## Encode Latency

Target:

* under 10ms

---

## Total End To End Latency

Target:

* under 80ms WAN

---

# Security

# Encryption

## Default

DTLS-SRTP

---

## Future

Optional E2EE

---

# Security Features

## Features

* IP restriction
* session audit
* secure clipboard
* permission isolation
* MFA integration

---

# Scalability

# Deployment Modes

## Single Node

Suitable for:

* personal usage
* small team

---

## Multi Node

Suitable for:

* enterprise
* cloud workstation
* GPU cluster

---

# Kubernetes Support

## Features

* stateless signaling
* distributed TURN
* autoscaling

---

# Future Roadmap

# Phase 1 — MVP

## Features

* browser remote desktop
* Windows support
* DXGI capture
* H264 NVENC
* WebRTC transport
* keyboard/mouse input
* clipboard sync

---

# Phase 2 — Production Ready

## Features

* Linux support
* macOS support
* adaptive bitrate
* audio streaming
* session reconnect
* file transfer

---

# Phase 3 — Advanced Optimization

## Features

* dirty region encoding
* AV1 support
* QUIC transport
* multi monitor
* mobile optimization

---

# Phase 4 — AI Workstation

## Features

* AI aware streaming
* semantic UI optimization
* remote GPU scheduling
* AI session assistant

---

# Competitive Positioning

# Competitors

## Remote Desktop

* Reemo
* NoMachine
* AnyDesk
* Parsec
* RustDesk

---

## Remote IDE

* code-server
* JetBrains Gateway
* Gitpod
* Coder
* Replit

---

# Positioning

9ed Remote Desktop fokus pada:

```text
Developer Native Remote Workstation
```

Bukan sekadar generic remote desktop.

---

# Differentiators

## 1. Browser First

Tidak wajib install client.

---

## 2. Developer Native

Integrated dengan:

* terminal
* git
* AI
* Monaco
* workspace

---

## 3. Hybrid Architecture

Gabungan:

* protocol level sync
* GPU video streaming

---

## 4. Self Hosted Friendly

Single binary deployment.

---

# Risks

# Technical Risks

## 1. Cross Platform Capture Complexity

Setiap OS punya API berbeda.

---

## 2. Browser Hardware Decode Compatibility

Tidak semua browser optimal.

---

## 3. NAT Traversal Complexity

TURN relay bisa mahal.

---

## 4. GPU Driver Compatibility

NVENC/AMF/QuickSync bisa inconsistent.

---

# Success Metrics

## Technical Metrics

* latency
* FPS
* reconnect success
* crash rate
* CPU usage
* bandwidth usage

---

## Product Metrics

* active sessions
* average session duration
* user retention
* remote usage frequency

---

# Final Vision

9ed Remote Desktop menjadi:

```text
Developer-first ultra low latency remote workstation platform
```

yang menggabungkan:

* remote IDE
* GPU workstation
* AI workspace
* browser accessibility
* self hosted infrastructure

ke dalam satu platform unified.
