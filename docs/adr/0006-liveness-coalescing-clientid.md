# ADR-0006: Liveness hybrid ping/pong + coalescing 60ms + clientId session connection

**Status**: accepted
**Date**: 2026-06-21

Tiga keputusan terkait connection resilience dan efficiency: **hybrid liveness ping/pong** (server RFC645 protocol ping + client app-level JSON ping), **stream coalescing 60ms window**, dan **clientId-keyed session connection** (pola paseo SessionConnection penuh). Menutup gap G4 (no liveness), G6 (no coalescing), dan enable transparent multi-device reconnect.

## Konteks

9ed saat ini: WS chat handler (`handleChatWebSocket`) tidak ada `SetReadDeadline`, tidak ada `SetPongHandler`, tidak ada app-level ping. Half-open connection tidak terdeteksi (G4). `chatStream.publish()` fan-out setiap event langsung, no coalescing (G6). WS connection = 1:1 dengan subscriber, no logical client identity, reconnect = new subscriber (no resume).

Paseo: app-level JSON ping/pong (10s interval, 15s timeout, 2 failure threshold), AgentStreamCoalescer 60ms window, SessionConnection keyed by clientId (multi-socket per clientId, 90s grace). Codex App Server: hello timeout 15s, bounded queue, thread/unsubscribe 30 menit grace.

## Keputusan

### Liveness hybrid ping/pong

- **Server → client**: RFC645 protocol ping via `gorilla/websocket` (`conn.WriteMessage(websocket.PingMessage, nil)`). Browser auto-respond protocol pong (no app code client-side). `SetPongHandler` + `SetReadDeadline(15s)`. Server-side liveness detection.
- **Client → server**: app-level JSON `{type:"ping", ts:<unix_ms>}` setiap 10s. Server balas `{type:"pong", ts:<unix_ms>}`. Client-side liveness detection (detect server dead). Client `SetReadDeadline` equivalent via JS timeout.
- **Failure threshold**: 2 consecutive failures sebelum teardown (hindari false positive transient blip). Pola paseo.
- **Reconnect**: exponential backoff + jitter (baseDelay 150ms, maxDelay 30s, pola paseo). Client reconnect dengan clientId (bagian session connection di bawah).
- **Configurable**: `LIVENESS_PING_INTERVAL=10s`, `LIVENESS_TIMEOUT=15s`, `LIVENESS_FAILURE_THRESHOLD=2`, `RECONNECT_BASE_DELAY=150ms`, `RECONNECT_MAX_DELAY=30s` di `.env`.

### Stream coalescing 60ms window

- **Batch** text/reasoning/tool_call_update events dalam **60ms window** sebelum fan-out + persist.
- **Merge** adjacent text tokens menjadi satu event (concat text, max length cap).
- **Collapse** tool_call lifecycle transitions (pending→running→completed jadi satu update jika dalam window yang sama).
- **Critical event** (permission_request, error, done, terminal_execute, session_resumed): **bypass coalescing**, langsung fan-out (critical = immediate, no delay).
- **Coalesce flush triggers**: 60ms timer, atau critical event arrival (flush pending batch dulu), atau turn end (flush).
- Pattern source: Paseo `AgentStreamCoalescer` (`agent-stream-coalescer.ts`), `AGENT_STREAM_COALESCE_DEFAULT_WINDOW_MS = 60`.
- **Configurable**: `STREAM_COALESCE_WINDOW=60ms` di `.env`.

### ClientId-keyed session connection

- **Client generate `clientId`** (UUID, stored in browser localStorage). Kirim saat WS connect: `{type:"hello", clientId:"<uuid>", sessionId:"<id>"}`.
- **Server: `Map<clientId, SessionConnection>`**. `SessionConnection` = `{clientId, session, sockets: Set[WebSocket], subscriberState, lastSeen}`.
- **Multiple socket per clientId** (multi-tab browser, multi-device): share subscriber state. Fan-out ke semua socket dalam connection.
- **Socket drop ≠ connection drop**: grace window (ADR-0003, 10 menit) menungu reconnect dengan clientId sama. Kalau reconnect → resume connection, reuse subscriber state (no re-fetch catch-up needed, subscriber state preserved).
- **Multi-device** (komputer A → B): B connect dengan clientId berbeda = subscriber baru (dapat replay via ADR-0002). Atau B pakai clientId sama (pair via QR/auth) = join connection yang sama.
- **Connection cleanup**: grace window expire (no socket + no activity 10 menit) → `SessionConnection.cleanup()` → unsubscribe dari `chatStream` → session teardown (ADR-0003). Grace window owner = SessionConnection (clientId), bukan per session, untuk konsistensi dengan ADR-0003.
- Pattern source: Paseo `SessionConnection` (`websocket-server.ts:289-296`), hello resume (`websocket-server.ts:108-1120`), fan-out (`websocket-server.ts:849-857`), disconnect grace (`websocket-server.ts:1289-1316`).

## Rejected alternatives

### Liveness

- **App-level JSON only (Opsi 1)**: client-side liveness OK, tapi server-side tidak optimal (JSON 40 bytes vs protocol ping 2 bytes). Symetric tapi less efficient server-side.
- **RFC645 protocol only (Opsi 2)**: server-side OK, tapi client-side blind (browser tidak expose protocol ping ke JS). Client tidak bisa detect server dead.

### Coalescing

- **No coalescing, raw fan-out (current)**: WebSocket + SQLite write churn tinggi pada fast token stream. UI flicker. Trigger Q11 backpressure drop lebih sering.

### ClientId

- **No clientId, session-level only (Opsi 2)**: reconnect = new subscriber + re-fetch catch-up (race condition, bandwidth waste). No multi-device identity.
- **ClientId optional (Opsi 3)**: dua codepath, inconsistency.

## Konsekuensi

- WS handler extend: hybrid ping/pong (RFC645 server-side + JSON client-side), deadline, failure threshold, reconnect backoff.
- `chatStream` extend: coalescer (60ms window, merge, collapse, critical bypass), batch persist + batch fan-out.
- `SessionManager` extend: `Map<clientId, SessionConnection>`, multi-socket per clientId, grace window per clientId, resume on reconnect.
- Client-side: clientId generation + localStorage, hello handshake, ping/pong JS, reconnect logic, coalesce-aware rendering (batched text merge).
- Env vars: `LIVENESS_PING_INTERVAL=10s`, `LIVENESS_TIMEOUT=15s`, `LIVENESS_FAILURE_THRESHOLD=2`, `RECONNECT_BASE_DELAY=150ms`, `RECONNECT_MAX_DELAY=30s`, `STREAM_COALESCE_WINDOW=60ms`.
- Effort: ~5-6 hari (liveness 1.5 hari, coalescing 2 hari, clientId 2.5 hari).
