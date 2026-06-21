# 9ed Chat & Agent

Glossary untuk domain chat dan AI agent di 9ed. Menjelaskan istilah yang spesifik untuk konteks ini; istilah pemrograman umum tidak dimasukkan.

## Language

**ACP (Agent Client Protocol)**:
Protokol JSON-RPC 2.0 over stdio untuk berkomunikasi dengan agent subprocess. Bukan standar publik; definisi tipe lengkap di `internal/chat/acp/protocol.go`.
_Avoid_: agent API, agent interface

**Agent**:
Sebuah CLI eksternal (claude, codex, opencode, copilot, dll) yang melakukan pekerjaan coding. Dideskripsikan oleh `AgentDescriptor` (ID, command, args, availability, ACP support).
_Avoid_: model, LLM, assistant

**Agent Descriptor**:
Metadata statis tentang sebuah agent: ID, command, args, apakah available di sistem, apakah support ACP. Dipakai untuk spawn subprocess.
_Avoid_: agent config, agent definition

**ChatSession**:
Satu percakapan aktif dengan satu agent. Bisa mode ACP (subprocess JSON-RPC) atau mode PTY (terminal interaktif). Memiliki lifecycle: create → prompt → stream events → done. Di-own oleh `SessionManager`.
_Avoid_: conversation, thread, chat

**Chat Event**:
Satu unit output dari ChatSession: text, thinking, tool_call, tool_call_update, plan, diff, done, error, permission_request, usage_update, dll. Di-stream via `chatStream` ke subscriber.
_Avoid_: message (terlalu overloaded; "message" = inbound user input), response

**chatStream**:
Fan-out layer per ChatSession. Mengubah event stream dari satu ChatSession menjadi broadcast ke multiple subscriber (WS client). Memiliki subscriber set, turn watchdog, tool fallback, dan turn recovery logic.
_Avoid_: chat channel, event bus

**Subscriber**:
Satu konsumen event dari chatStream. Saat ini 1:1 dengan satu WebSocket connection. Menerima event via buffered channel (kapasitas 256).
_Avoid_: listener, observer, client (terlalu overloaded)

**Live Session**:
ChatSession yang subprocess-nya masih berjalan dan ada di `SessionManager` (in-memory). Berbeda dari Session Record yang hanya data persisten di SQLite.
_Avoid_: active session, running session

**Session Record**:
Data persisten sebuah sesi di SQLite (`chat_sessions` table): ID, agent ID, work dir, ACP session ID, status, title. Bisa ada tanpa Live Session (sesi yang sudah ditutup tapi masih bisa di-resume).
_Avoid_: session entry, session data

**Resume**:
Membuka kembali Session Record sebagai Live Session dengan memanggil ACP `session/resume` (atau CLI `--resume` untuk PTY mode). Agent melanjutkan konteks percakapan sebelumnya.
_Avoid_: restore (restore = mengambil Session Record tanpa necessarily spawn ulang), reopen

**ACP Session ID**:
Identifier yang diberikan agent saat `session/new` atau `session/resume`. Dipakai untuk resume di kemudian hari. Persisten di Session Record. Tidak sama dengan 9ed Live Session ID (UUID internal 9ed).
_Avoid_: agent session, thread ID

**Turn**:
Satu siklus prompt → agent berpikir/bekerja → done. Dimulai saat user kirim message, berakhir saat agent kirim `done` event. `chatStream.StartTurn()` mengatur watchdog dan recovery logic per turn.
_Avoid_: round, cycle

**Mode ACP**:
Mode ChatSession dimana agent dikomunikasikan via ACP protocol (JSON-RPC over stdio). Structured events, support multi-client concurrent (via chatStream fan-out).
_Avoid_: ACP mode, structured mode

**Mode PTY**:
Mode ChatSession dimana agent dikomunikasikan via terminal interaktif (PTY). Input = ketik ke terminal, output = parse teks mentah. Tidak support multi-client concurrent (konflik input).
_Avoid_: terminal mode, CLI mode

## Multi-Client Terms

**Subscriber**:
Satu konsumen event dari chatStream. 1:1 dengan satu WebSocket connection. Menerima event via buffered channel (kapasitas 256). Di-drop diam-diam jika channel penuh (gap yang akan di-fix).
_Avoid_: listener, observer

**Concurrent Multi-Client**:
Multiple client (browser/device) connect ke ChatSession yang sama secara bersamaan, menerima live event stream real-time. Superset dari Sequential Resume. Target arsitektur 9ed (sesuai keputusan grilling Q2).
_Avoid_: multi-user (terlalu overloaded; multi-user = multi-identitas, bukan multi-koneksi), collaboration (implies concurrent editing, terlalu spesifik)

**Sequential Resume**:
Client B melanjutkan ChatSession setelah client A tutup, melihat history + bisa lanjut chat. Tidak ada A dan B aktif bersamaan. Kasus khus dari Concurrent Multi-Client.
_Avoid_: session handoff, session transfer

**Viewer**:
Client yang connect ke ChatSession PTY tapi tidak bisa mengirim input. Hanya melihat output stream. Default mode untuk PTY multi-client (mirip ttyd readonly default).
_Avoid_: read-only client (terlalu teknis), observer

**Primary**:
Client yang memegang kontrol input pada ChatSession PTY. Hanya satu primary pada satu waktu. Viewer bisa "request control" untuk menjadi primary.
_Avoid_: driver, controller, master

**Request Control**:
Mekanisme viewer untuk meminta menjadi primary. Primary saat ini dilepas (atau ditahan sampai idle). Setara dengan "remote control" di screen sharing tools.
_Avoid_: take over (implies forceful), steal focus

**Grace Window**:
Periode setelah subscriber terakhir disconnect di mana ChatSession tetap hidup menunggu reconnect. 9ed target: 90s-5menit (antara paseo 90s dan Codex App Server 30 menit).
_Avoid_: timeout, TTL (terlalu generik)

**Backpressure Signal**:
Notifikasi eksplisit ke client saat subscriber lambat / channel penuh, sebagai pengganti silent drop. Client tahu kapan retry. Pola dari Codex App Server (`-32001` error) dan paseo (dual-threshold).
_Avoid_: flow control (terlalu teknis), throttling

## Transport Terms

**Transport**:
Mekanisme koneksi antara client dan ChatSession. Saat ini: WebSocket. Future-proof target: WebSocket + SSH (sebagai alternatif transport, bukan agent mode).
_Avoid_: connection, link

**Transport-Agnostic**:
Property ChatSession interface yang tidak terikat ke transport spesifik. ACP dan PTY sudah transport-agnostic (keduanya via subprocess, di-serve via WS). SSH akan jadi transport ketiga.
_Avoid_: protocol-agnostic (beda konsep)

## Visual Streaming Terms

**Frame Source**:
Interface yang produce raw frame pixels dari sebuah surface. Pluggable per-surface. Browser = CDP `Page.startScreencast` via Playwright `NewCDPSession`. Remote desktop = screen capture native (DXGI/X11/ScreenCaptureKit).
_Avoid_: capture source, screen source

**Visual Stream Strategy**:
Interface yang encode frame dan distribusi ke subscriber via pion/webrtc. Pluggable. Dua implementasi: JPEG Tile Diff dan H264 Full Frame. Strategy dipilih per-surface, transport tetap unified (pion).
_Avoid_: encoder strategy, rendering mode

**Tile**:
Region persegi dari frame (128-256px, config per-platform) yang di-encode terpisah sebagai unit. Dipakai di JPEG Tile Diff strategy.
_Avoid_: block, chunk, segment

**Tile Diff**:
Pola rendering yang kirim HANYA tiles yang berubah (diff) dari frame sebelumnya, bukan full frame. Screen mostly-static = sedikit tiles berubah = bandwidth rendah. Pola produksi 9remote.
_Avoid_: incremental update, partial frame, dirty region

**JPEG Tile Diff** (Strategy A):
Visual stream strategy yang encode tiap tile sebagai JPEG (`image/jpeg` stdlib, pure Go) + kirim changed tiles via pion DataChannel. Default untuk browser collaborative. Option untuk remote desktop. Pure Go, no dependency, lower latency, excellent untuk mostly-static screen content. Poor untuk full motion.
_Avoid_: tile streaming, JPEG streaming

**H264 Full Frame** (Strategy B):
Visual stream strategy yang encode full frame sebagai H264 via ffmpeg subprocess + kirim via pion video track. Option untuk remote desktop (full motion use case). External dependency (ffmpeg auto-download), encode latency, NAL unit parser. Excellent untuk full motion, universal codec, hardware decode di client.
_Avoid_: H264 streaming, video track streaming

**Frame**:
Satu unit raw pixel data dari Frame Source (RGBA/BGRA bytes + width + height + timestamp). Input untuk Visual Stream Strategy. Berbeda dari "Chat Event" (event stream ACP) dan "tile" (region encoded).
_Avoid_: image, picture, screenshot

**NAL Unit**:
Network Abstraction Layer unit. Unit data H264 yang dipush ke pion video track. Dari ffmpeg stdout, perlu parsing boundary via start code `00 00 00 01`. Hanya relevant untuk Strategy B.
_Avoid_: H264 packet, video packet

**Input Handling Layer**:
Layer yang handle input events (click, type, scroll, gesture) dari client ke server. Independent dari Visual Stream Strategy (codec). pion DataChannel untuk transport. Client-side gesture mapping (long press = right-click untuk Ancodebuddy). Server-side platform input injection (SendInput/XTest/CGEventPost). Klik kan + touch Ancodebuddy teratasi di sini, bukan di codec.
_Avoid_: input forwarding, event handling

**Input Arbitration**:
Mekanisme untuk collaborative multi-client: hanya primary yang input diteruskan ke Frame Source. Viewer = read-only. Primary/secondary dengan request control. Sama untuk browser, terminal, remote desktop.
_Avoid_: input lock, input control

**Gesture Mapping**:
Client-side translation dari touch/gesture ke input events. Long press → right-click, two-finger tap → right-click, single tap → left-click, two-finger drag → scroll. Penting untuk Ancodebuddy compatibility (tidak punya native right-click).
_Avoid_: touch mapping, gesture translation

## Resilience Terms

**Epoch**:
UUID yang identify versi timeline sebuah ChatSession. Berubah saat session resume (ACP subprocess mati + spawn ulang). Dipakai untuk stale cursor detection (ADR-0002). Client dengan cursor.epoch ≠ current epoch = re-fetch tail.
_Avoid_: timeline version, session version

**Cursor**:
Posisi dalam timeline yang client pegang untuk catch-up fetch. Format `{epoch, seq}`. Dipakai di `fetchTimeline` RPC (ADR-0002) dengan direction after/before/tail.
_Avoid_: bookmark, position

**Replay-on-Subscribe**:
Mekanisme kirim recent events (last N) ke subscriber baru saat connect ke chatStream. Bukan full history load, tapi recent context untuk immediate render. Client lanjut catch-up via cursor jika perlu (ADR-0002).
_Avoid_: history replay, backfill

**Grace Window**:
Periode setelah subscriber terakhir disconnect di mana ChatSession + subprocess tetap hidup menunggu reconnect. Default 10 menit, configurable via `SESSION_GRACE_WINDOW`. Kalau reconnect dengan clientId sama (ADR-0006) → resume. Kalau tidak → teardown.
_Avoid_: timeout, TTL, idle timer

**Prioritized Channel**:
Dua channel per subscriber: priority (critical event: permission_request, error, done, terminal_execute, session_resumed — never drop, agent Cancel jika penuk) + bulk (normal event: text, tool_call_update, thinking — drop oldest, client catch-up via seq gap). Backpressure handling (ADR-0003).
_Avoid_: priority queue, dual queue

**Soft Lock**:
Mekanisme collaborative PTY input: saat client mulai ngetik, acquire lock (2 detik TTL, auto-renew) cegah client lain ngetik di pane itu. Auto-release on idle atau TTL expire. Per-pane granularity. Mencegah garbled simultanuous typing (ADR-0005).
_Avoid_: input lock, typing lock

**ClientId**:
UUID yang identify logical client identity (generate di browser localStorage, survive socket drop). Multiple socket bisa share clientId (multi-tab/device). Reconnect dengan clientId sama = resume SessionConnection (ADR-0006). Beda dari sessionId (identify ChatSession) dan socketId (identify WS connection).
_Avoid_: device id, session id (terlalu overloaded)

**SessionConnection**:
Logical client connection keyed by clientId. Holds `{clientId, session, sockets: Set, subscriberState, lastSeen}`. Multiple WS socket per SessionConnection (multi-tab). Grace window per SessionConnection (ADR-0003, ADR-0006).
_Avoid_: client session, connection

**Coalescing**:
Batch text/reasoning/tool_call_update events dalam 60ms window sebelum fan-out + persist. Merge adjacent text tokens, collapse tool_call lifecycle transitions. Critical event bypass coalescing (immediate). Reduce WebSocket + SQLite load (ADR-0006).
_Avoid_: batching, throttling, buffering
