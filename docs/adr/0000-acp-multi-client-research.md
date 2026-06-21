# Research: ACP Agent Robustness & Multi-Client Sync

**Date**: 2026-06-21
**Type**: Research baseline (pre-decision). Bukan ADR keputusan.
**Status**: Reference document for ongoing grilling/discussion.

Riset mendalam atas 4 referensi (paseo, paseo-relay, zed, tycho) vs kondisi 9ed saat ini, terkait koneksi AI agent via ACP dan multi-client sync. Dokumen ini menjadi anchor diskusi untuk pekerjaan perbaikan.

**Companion document**: `docs/research/multi-client-landscape.md` berisi riset lanskap 9+ implementasi dunia nyata (ttyd, sshx, Codex App Server, opencode-multiplexer, Mosh, tmux/tmate, JupyterLab, code-server, Daytona) yang memperdalam rekomendasi di dokumen ini.

---

## 1. 9ed — Kondisi Saat Ini

### 1.1 Arsitektur ACP yang sudah ada

9ed punya **dua mode agent**:

| Mode | Implementasi | Agent | Transport |
|------|-------------|-------|-----------|
| ACP | `internal/chat/acp/adapter.go` + `client.go` | opencode, copilot, generic `--acp` agents | JSON-RPC 2.0 over stdio |
| PTY | `internal/chat/pty_session.go` | claude, codex (CLI langsung) | PTY (terminal interaktif) |

**Komponen kunci:**

- `acp.Adapter` (`adapter.go`): spawn subprocess, `initialize()`, `NewSession()`, `ResumeSession()` (via ACP `session/resume` jika agent mendukung), `Prompt()`, `Cancel()`. Mengelola 1 subprocess per adapter.
- `acp.Client` (`client.go`): JSON-RPC 2.0 client over stdin/stdout pipes. Request/response correlation via `pending map[int64]chan *Response`. Notification + request channels. `readLoop()` dengan `bufio.Scanner` (max 10MB). Saat stdout EOF, semua pending request di-close dengan error.
- `SessionManager` (`session_manager.go`): `map[string]ChatSession` in-memory. `Create()`, `Resume()`, `Get()`, `Remove()`, `LinkRecordID()`, `LiveIDForRecordID()`. Tidak ada TTL/grace period; `Remove()` langsung `Close()` session.
- `ChatStore` (`store.go`): SQLite (`~/.9ed/ide.db`), WAL mode, MaxOpenConns=1. Persist session records + chat history + workspace state.
- `chatStream` (`chat_stream.go`): **sudah punya fan-out multi-subscriber** — `subscribers map[*chatSubscriber]struct{}`. Setiap subscriber dapat channel buffered 256. `publish()` fan-out ke semua subscriber. Ini pondasi multi-client yang sudah ada.
- `chatStreamRegistry`: `map[string]*chatStream` per sessionID. `GetOrCreate()` reuse stream yang ada. `Touch()` update latestID.

### 1.2 WebSocket handler (`chatapi.go:1333`)

```
GET /ws/chat/{sessionID}
```

Flow: `GetOrCreate` stream → `Subscribe()` → goroutine baca dari `sub.C` → `conn.WriteJSON(evt)`. Inbound: `ReadJSON` loop untuk message/cancel/permission/config.

### 1.3 Yang sudah berfungsi (lebih baik dari yang diasumsikan)

1. **Fan-out multi-subscriber sudah ada** di `chatStream`. Multiple WS client ke session ACP yang sama secara teknis bisa connect dan menerima event yang sama.
2. **ACP `session/resume` sudah diimplementasi** (`adapter.go:ResumeSession`). Capability check via `SupportsResume()`.
3. **SQLite persistence** untuk history + state. Restore flow ada (`handleChatRestore`).
4. **Live vs Record mapping** (`LiveIDForRecordID`, `LinkRecordID`) untuk korelasi session live dengan record DB.

### 1.4 Lubang / kelemahan vs referensi (akar instabilitas + multi-client)

| # | Masalah | Lokasi kode | Dampak |
|---|---------|-------------|--------|
| G1 | **Tidak ada replay saat re-subscribe.** Client B connect mid-turn hanya dapat event baru, bukan event yang sudah lewat. Tidak ada epoch/seq cursor. | `chatStream.Subscribe()` hanya daftar; tidak replay | Client B melihat history gap; harus manual fetch `/api/chat/history` |
| G2 | **Subscriber lambat langsung di-drop diam-diam.** `publish()` punya `default: delete(sub); close(sub.C)` — jika channel buffered 256 penuh, subscriber disconnect tanpa notifikasi. | `chat_stream.go:publish()` | Koneksi mobile flaky = kehilangan event diam-diam, UI diam padahal stream mati |
| G3 | **Tidak ada grace window.** WS putus → `Unsubscribe` langsung. Jika subscriber terakhir, stream tetap hidup (session di manager) tapi tidak ada mekanisme deteksi "client reconnect dalam window X". | `handleChatWebSocket` defer `Unsubscribe` | Reconnect cepat setelah network blip = state hilang, harus re-fetch manual |
| G4 | **Tidak ada liveness ping/pong app-level.** Browser WS API tidak expose RFC6455 protocol ping. Half-open connection tidak terdeteksi. | WS handler tidak ada ping/pong | Connection terlihat hidup tapi sebenarnya mati; event tidak sampai, tidak ada reconnect trigger |
| G5 | **Subprocess ACP mati = stream mati, tidak auto-resume.** `session.Done()` close → `chatStream.run()` kirim `done` event → stream selesai. Tidak ada spawn ulang via `session/resume`. | `chat_stream.go:run()` + `acp.Client.readLoop()` EOF | Agent crash/network error = session berakhir permanen, user harus manual resume |
| G6 | **Tidak ada coalescing.** Fast token stream di-fan-out mentah per event. | `chatStream.publish()` langsung | WebSocket + SQLite write load tinggi pada stream cepat; UI churn |
| G7 | **PTY-mode agent tidak multi-client-ready.** `ptySession` kirim input ke 1 PTY; tidak ada broadcast ke multi-subscriber dengan sinkronisasi state terminal. Multi-client PTY = konflik input. | `pty_session.go` | claude/codex via PTY tidak bisa multi-client concurrent |
| G8 | **`SessionManager.Remove()` langsung `Close()`.** Tidak ada grace period sebelum kill subprocess. | `session_manager.go:Remove()` | Close tab/browser = agent subprocess langsung mati, tidak ada window untuk reconnect |

---

## 2. Paseo — Pola Agent Orchestration + Multi-Client

**Stack**: TypeScript/Node.js monorepo. Daemon lokal + client (mobile/desktop/web/CLI).

### 2.1 Agent connection + session model

- `AgentManager` (`packages/server/src/server/agent/agent-manager.ts:507`): single owner of agent lifecycle. Holds `clients: Map<AgentProvider, AgentClient>`, `agents: Map<agentId, LiveManagedAgent>`, in-memory `InMemoryAgentTimelineStore`, durable `AgentTimelineStore`, dan `Set<SubscriptionRecord>`.
- States: `initializing → idle → running → idle (or error → closed)`.
- Setiap provider (Claude, Codex, Copilot, OpenCode, Pi) abstracted behind `AgentClient` interface (`agent-sdk-types.ts:649-690`): `createSession` / `resumeSession` / `listModels` / `isAvailable`. Masing-masing spawn subprocess sendiri.
- **Agent = single-instance, shared.** Per-client view di-multiplex, bukan di-duplikat.

### 2.2 Multi-client sync — DUA LAPIS fan-out

**Layer 1: AgentManager → Sessions (pub/sub dengan replay)**

`AgentManager.subscribe(callback, options)` (`agent-manager.ts:675-710`) daftar subscriber. Saat subscribe, **replay current agent state** kecuali `replayState: false`. `dispatch(event)` (`agent-manager.ts:3576-3607`) fan-out setiap state change + timeline event ke semua subscriber.

**Layer 2: SessionConnection → multiple sockets (multi-client sync sesungguhnya)**

`SessionConnection` (`websocket-server.ts:289-296`) keyed by `clientId`, holds `Set<WebSocketLike>`:

```ts
interface SessionConnection {
  session: Session;
  clientId: string;
  sockets: Set<WebSocketLike>;
}
```

- Dua map: `sessions: Map<WebSocket, SessionConnection>` (per-socket) dan `externalSessionsByKey: Map<clientId, SessionConnection>` (per-client, survives socket drop).
- **Hello dengan known `clientId`** (`websocket-server.ts:108-1120`): resume existing connection, `existing.sockets.add(ws)`. Client baru dengan `clientId` sama = join session yang sama.
- **Fan-out per connection** (`websocket-server.ts:849-857`): `sendToConnection` loop ke semua socket di connection. Setiap agent event dikirim ke semua socket.
- **State consistency by construction**: semua `Session` subscribe ke `AgentManager` yang sama; setiap transisi via `dispatch`; semua subscriber lihat event yang sama urutan sama.

### 2.3 Session resumption (timeline catch-up)

Dua jalur (didokumentasikan di `docs/timeline-sync.md`):

1. **Live stream** — `agent_stream` messages dengan `seq`, `epoch`, `timestamp`.
2. **Authoritative history** — `fetch_agent_timeline_request` RPC (`session.ts:8292-8390`). Selalu return full projected timeline items (bukan delta). Support `direction: "tail" | "after" | "before"`, `cursor: { epoch, seq }`, paging via `limit` / `hasNewer` / `endCursor`.

**Timeline store** (`agent-timeline-store.ts`):
- `append()` assign monotonic `seq = state.nextSeq++` (`agent-timeline-store.ts:283-294`).
- Setiap agent punya `epoch` (UUID) yang berubah saat timeline di-reset.
- **Stale cursor handling** (`agent-timeline-store.ts:239-251`): jika `cursor.epoch !== state.epoch`, return `reset: true, staleCursor: true`. Client re-fetch tail page.
- **Catch-up invariant**: live stream untuk immediacy; `fetch_agent_timeline_request` untuk correctness. Client catch-up dengan paging `after` cursor sampai `hasNewer: false`.

### 2.4 Resilience

| Mekanisme | Detail | Referensi |
|-----------|--------|-----------|
| **Reconnect grace window** | 90 detik. Last socket drop → connection + Session tetap hidup 90s menunggu reconnect dengan `clientId` sama. | `websocket-server.ts:300, 1289-1316` |
| **Multi-socket resilience** | Salah satu socket drop, lainnya tetap → `SessionConnection` hidup. Mobile drop, desktop tetap stream. | `websocket-server.ts:1317-1333` |
| **Liveness heartbeat** | App-level JSON `ping`/`pong` (bukan RFC6455, karena browser/RN tidak expose). 10s interval, 15s timeout. | `daemon-client.ts:763-764, 1735-1762` |
| **Failure threshold** | 2 consecutive failures (`LIVENESS_FAILURE_RECONNECT_THRESHOLD = 2`) sebelum teardown. Hindari reconnect storm. | `daemon-client.ts:765` |
| **Reconnect backoff** | Exponential, `baseDelay=150ms`, `maxDelay=300ms` (client); relay control: linear capped 30s. | `daemon-client.ts:759-760, 4878-4891` |
| **Hello timeout** | 15s server-side, close 4001 jika tidak hello. | `websocket-server.ts:300, 884-906` |
| **Stream coalescing** | 60ms window per agent untuk `assistant_message`/`reasoning`/`tool_call`. | `agent-stream-coalescer.ts`, `agent-manager.ts:536-548` |
| **Terminal backpressure** | Dual-threshold: 256KB byte + 4MB `bufferedAmount`. Relay socket (null bufferedAmount) → unconditional snapshot fallback. | `terminal-session-controller.ts:855-875` |
| **Lazy agent resume** | `ensureAgentLoaded` resume on first reference, bukan eager on boot. Dedup map `pendingAgentInitializations`. | `agent-loading.ts:20-70` |
| **Async persistence** | Setiap `agent_state` event → async write ke disk, tidak pernah block fan-out hot path. | `persistence-hooks.ts:53-72` |

### 2.5 Relay (paseo-relay) — peran dan protokol

- Relay = **optional encrypted bridge** untuk remote access. Zero-knowledge: route encrypted bytes, tidak bisa baca konten.
- Daemon hold Curve25519 keypair. Client dapat public key via pairing QR/URL.
- E2E: NaCl `box` (XSalsa20-Poly1305).
- **Daemon membuka 1 outbound data socket per client `connectionId`**, fed ke WS server yang sama via `attachExternalSocket`. Relay hanya route bytes by `connectionId`; daemon treat relay socket identik dengan direct socket.
- Relay buffer up to 200 frames per `connectionId` saat daemon data socket belum ada. Nudge/reset control socket jika half-open (10s sync, 5s force-close).
- **Discovery**: client discover daemon by `serverId` (encoded di pairing offer). Relay route by `serverId` ke Durable Object via `idFromName`.

---

## 3. Paseo-Relay — Go Zero-Knowledge Relay

**Stack**: Go, single `main.go` (~700 baris), `gorilla/websocket`.

### 3.1 Protokol v2 — tiga endpoint WS

```
GET /ws?serverId=<id>&role=server&v=2                    — daemon control socket (1 per session)
GET /ws?serverId=<id>&role=server&connectionId=<c>&v=2   — daemon data socket (1 per client connectionId)
GET /ws?serverId=<id>&role=client[&connectionId=<c>]&v=2 — client socket (many per connectionId)
GET /health — {"status":"ok","version":"<version>"}
```

### 3.2 Struktur data

- `session`: control `conn` + `pipes map[connectionId]*pipe`. `pipe` = `serverData *conn` + `clients []*conn` + `pending *frameBuffer`.
- `registry`: `sessions map[serverId]*session`, max 10.000 sessions. Eviction loop 5 menit.
- `frameBuffer`: max 200 items / 32MB. Prefix 1-byte msgType untuk replay dengan WS message type benar.

### 3.3 Routing

- **Client → Server**: forward ke `server:<connectionId>` socket. Jika belum ada, buffer.
- **Server → Client**: forward ke semua `client:<connectionId>` sockets.
- **Control plane**: client connect → `{type:"connected", connectionId}` ke control. Daemon buka data socket. Jika daemon tidak reaksi 10s → `{type:"sync", connectionIds}`. Jika masih 5s → force-close control (daemon reconnect).

### 3.4 Relevansi untuk 9ed

- **Langsung portable** ke stack 9ed (Go + gorilla/websocket, sama persis).
- 9ed saat ini pakai `internal/tunnel/` (cloudflared/bore) untuk akses publik — itu TCP-level tunnel, application traffic diteruskan apa adanya. Paseo-relay adalah **application-level relay** dengan routing per-session dan buffering. Berbeda layer, bisa komplementer.
- Jika 9ed ingin relay aplikasi-level (misal untuk multi-device tanpa cloudflare, atau untuk routing session spesifik), paseo-relay bisa di-adopsi hampir verbatim.

---

## 4. Zed — CRDT Editor Collaboration + ACP

**Stack**: Rust monorepo. Editor + collab server.

### 4.1 ACP connection

- Crate `acp_thread` dan `acp_tools` ada di `crates/`. Crate `agent` (`crates/agent/`) dan `agent_servers` juga ada.
- Zed mengintegrasikan ACP (Agent Client Protocol) untuk external agents, tetapi modelnya berbeda dari 9ed/paseo: Zed adalah editor desktop, ACP agent berjalan lokal per-editor-instance, bukan daemon shared multi-client.

### 4.2 Multi-client collaboration — CRDT model

- Zed menggunakan **CRDT** (Conflict-free Replicated Data Types) untuk collaborative editing.
- Crate `text` (CRDT text buffer), `sum_tree` (B-tree dengan monoid summary), `channel` (collab channel).
- **Replica model**: setiap client punya replica ID. Operasi diberi Lamport timestamp + version vector.
- **Late-join catch-up**: client baru request operation log dari collab server, replay operasi sampai current state.
- **Collab server (tycho dalam konteks Zed asli, BUKAN repo tycho di references/)**: fan-out operasi ke semua replica. Server adalah single source of truth untuk operation log, tapi setiap replica bisa apply operasi secara deterministik.

### 4.3 Relevansi untuk 9ed

- **Model CRDT overkill untuk 9ed.** 9ed tidak collaborative text editing; 9ed butuh sync chat event stream (append-only log), bukan concurrent text edit. CRDT menyelesaikan masalah yang tidak dimiliki 9ed.
- **Pelajaran yang relevan**: append-only operation log dengan monotonic seq untuk catch-up (sama dengan pola paseo timeline). Tapi implementasi CRDT penuh (sum_tree, version vector) tidak diperlukan.
- Zed's ACP integration pattern (agent sebagai local subprocess per editor) tidak applicable untuk 9ed yang sudah punya arsitektur server-agent.

---

## 5. Tycho — Ruby TUI Agent Management

**Catatan penting**: `references/tycho` BUKAN collab server Zed. Tycho adalah "HQ" — terminal dashboard Ruby (Bubbletea/Lipgloss) untuk monitoring Kamal-deployed projects + managed coding agents. **Single-operator, tidak ada multi-client sync.**

### 5.1 Agent connection model

- Managed agent via CLI subprocess: Codex (`codex exec`) dan Claude-compatible (`--output-format stream-json`).
- **Bukan ACP**. Pakai CLI flags + parse JSON stream stdout.
- `ManagedAgent` (`lib/hq/domain/managed_agent.rb`): spawn, log, message, structured result, inquiry state.

### 5.2 Session resumption (pola native resume)

- **Native session ID persistence**: Claude-like agents dapat generated `--session-id` pada first run, lalu `--resume` setelahnya. Codex capture first `thread_id` dari JSON stream, lalu `codex exec resume`.
- Persisted di `~/.tycho/logs/managed_agents.json`.
- **Setelah native session known**: follow-up run kirim hanya latest user message (bukan replay full memory window). `memory.jsonl` tetap canonical transcript untuk first run / agent tanpa native session.
- Trade-off: Codex resumed runs tidak bisa pass `--output-schema` via resume subcommand.

### 5.3 Canonical transcript pattern

- `memory.jsonl` = canonical event log per agent. Bootstrapped dari existing messages, bounded replay.
- `raw.log` = live streaming during active run.
- `conversation.log` / `system.log` = human-readable artifacts.
- Hybrid rendering: history dari `memory.jsonl`, live dari `raw.log`. Saat run selesai, `capture_run_memory!` commit full assistant messages + tool summaries ke `memory.jsonl`.

### 5.4 Relevansi untuk 9ed

- **Pola native session resume relevan**: 9ed sudah punya `acp.Adapter.ResumeSession()` (ACP `session/resume`). Pola tycho (persist native session ID, resume dengan hanya latest message) konsisten dengan arah 9ed. Bedanya: 9ed pakai ACP protocol (structured), tycho pakai CLI flags (ad-hoc).
- **Canonical transcript pattern**: 9ed sudah punya SQLite sebagai canonical store (setara `memory.jsonl`). Tidak perlu migrasi ke jsonl.
- **Tidak ada pelajaran multi-client** dari tycho (single-operator by design).
- **Hook system** (`agent.run.*`, `agent.inquiry.available`) menarik tapi out-of-scope untuk masalah ACP robustness.

---

## 6. Sintesis — Peluang Perbaikan untuk 9ed

Urut berdasarkan dampak terhadap instabilitas + multi-client, dengan estimasi effort.

### Tier 1: Stabilitas dasar (prasyarat multi-client)

| # | Perbaikan | Menutup gap | Effort | Pola referensi |
|---|-----------|-------------|--------|----------------|
| F1 | **App-level ping/pong di WS chat** | G4 | ~1 hari | paseo liveness heartbeat |
| F2 | **Subscriber slow-drop → backpressure awareness** | G2 | ~1 hari | paseo terminal backpressure dual-threshold |
| F3 | **Grace window sebelum stream teardown** | G3, G8 | ~1-2 hari | paseo 90s reconnect grace |
| F4 | **Auto-resume subprocess ACP saat crash** | G5 | ~2-3 hari | paseo lazy resume + tycho native resume |

### Tier 2: Multi-client sync

| # | Perbaikan | Menutup gap | Effort | Pola referensi |
|---|-----------|-------------|--------|----------------|
| F5 | **Replay on subscribe** (kirim recent events ke subscriber baru) | G1 | ~1-2 hari | paseo AgentManager replay on subscribe |
| F6 | **Epoch + monotonic seq per session timeline** + cursor-based catch-up RPC | G1 | ~3-4 hari | paseo timeline store + `fetch_agent_timeline` |
| F7 | **Stream coalescing** (60ms window untuk text/tool events) | G6 | ~1 hari | paseo AgentStreamCoalescer |

### Tier 3: Arsitektur (opsional, tergantung scope)

| # | Perbaikan | Menutup gap | Effort | Pola referensi |
|---|-----------|-------------|--------|----------------|
| F8 | **ClientId-keyed session connection** (multi-socket per logical client) | multi-device | ~3-5 hari | paseo SessionConnection |
| F9 | **Application-level relay** (opsional, untuk remote multi-device tanpa cloudflare) | remote access | ~3-5 hari | paseo-relay (Go, portable verbatim) |
| F10 | **PTY-mode multi-client** (broadcast scrollback + input arbitration) | G7 | ~5-8 hari, risiko tinggi | tidak ada referensi langsung; pola terminal multiplexer |

### Catatan prioritas

- **F1-F4 (Tier 1)** adalah prasyarat. Tanpa liveness detection, backpressure, grace window, dan auto-resume, multi-client tidak akan stabil terlepas dari sync mechanism.
- **F5-F7 (Tier 2)** adalah inti multi-client sync. F6 (epoch+seq cursor) adalah yang paling kompleks tapi paling berdampak untuk "client B catch-up setelah connect mid-turn".
- **F8-F10 (Tier 3)** opsional. F8 (clientId) memberi experience "join session dari device berbeda". F9 (relay) hanya jika cloudflared/bore dirasa kurang. F10 (PTY multi-client) risiko tinggi, nilai rendah.

---

## 7. Pertanyaan terbuka untuk grilling

Dokumen ini belum menjawab:

1. **Mode fokus**: ACP-only, PTY-only, atau keduanya? (Pertanyaan grilling #1, sudah diajukan)
2. **Target multi-client**: concurrent (A dan B terbuka bersamaan) vs sequential resume (B setelah A tutup)?
3. **Skema persistence**: SQLite sudah ada. Apakah timeline store terpisah (paseo-style) atau extend table yang ada?
4. **ClientId**: apakah 9ed perlu konsep logical client identity (paseo `clientId`), atau cukup session-level fan-out?
5. **Relay**: apakah application-level relay (paseo-relay) diperlukan, atau cloudflared/bore (TCP tunnel) cukup?
6. **Protocol evolution**: WS message shape `{type, data}` saat ini. Apakah perlu capability handshake (paseo-style) untuk evolusi protocol?

Keputusan-keputusan ini akan di-resolve satu per satu selama grilling, dan setiap keputusan yang memenuhi kriteria ADR (hard to reverse + surprising + real trade-off) akan dicatat sebagai ADR terpisah (`0001+`).
