# ADR-0003: Grace window 10 menit configurable + prioritized backpressure channel

**Status**: accepted
**Date**: 2026-06-21

Dua keputusan terkait session lifecycle dan event delivery: **grace window default 10 menit (configurable via `.env`)** sebelum session teardown, dan **prioritized backpressure channel** (critical event never drop, bulk event drop oldest + seq gap catch-up, agent Cancel jika priority channel penuk). Menutup gap G2 (silent subscriber drop) dan G3/G8 (no grace window).

## Konteks

9ed saat ini: `SessionManager.Remove()` langsung `Close()` subprocess tanpa grace window. `chatStream.publish()` drop subscriber diam-diam jika channel 256 penuk (`default: delete(sub); close(sub.C)`). Client tidak tahu koneksi mati. Critical event (permission_request, error, done) bisa hilang.

Paseo: 90s grace window. Codex App Server: 30 menit unload grace. Paseo terminal backpressure: dual-threshold (256KB byte + 4MB bufferedAmount). Codex: bounded queue dengan `-3201` explicit error.

## Keputusan

### Grace window

- Default **10 menit**. Configurable via `SESSION_GRACE_WINDOW=10m` di `.env`.
- Socket terakhir dalam SessionConnection (ADR-0006) disconnect → SessionConnection + session + subprocess tetap hidup selama grace window menungu reconnect dengan clientId sama (ADR-0006). Grace window adalah **per SessionConnection (clientId)**, bukan per session, untuk konsistensi dengan ADR-0006.
- Kalau reconnect dalam window → resume SessionConnection (ADR-0006 clientId). Kalau tidak → teardown subprocess.
- Multi-device: kalau ada clientId lain masih subscribed, session tetap hidup (tidak masuk grace window).
- 10 menit = sweet spot: cukup untuk pindah device santai + network blip, tidak terlalu boros memory (ACP ~30-50MB, PTY ~10-20MB per idle session).

### Prioritized backpressure channel

- **Dua channel per subscriber**: priority (critical event) + bulk (normal event).
- **Critical event** (permission_request, error, done, terminal_execute, session_resumed): ke priority channel. **Never drop**. Kalau priority channel penuk > threshold → **backpressure ke agent**: `session.Cancel()` + emit `done` dengan `stopReason: "client_backpressure"`. Agent berhenti, user bisa re-prompt.
- **Bulk event** (text streaming, tool_call_update, thinking, plan): ke bulk channel. **Drop oldest** kalau penuh. Client detect seq gap (ADR-0002) → fetch catch-up via cursor. Subscriber tetap hidup (no disconnect).
- Channel capacity: priority 64, bulk 256 (configurable).

Pattern source: Codex App Server bounded queue `-3201` (explicit backpressure), Paseo dual-threshold (adapted to event-based ACP), 9ed seq gap recovery (ADR-0002).

## Rejected alternatives

- **Explicit error + drop subscriber (Opsi 1)**: critical event bisa hilang. Reconnect storm.
- **Drop oldest semua (Opsi 2 simple)**: critical event (permission_request) bisa didrop → user tidak bisa approve → agent hang forever. Correctness failure.
- **Hybrid time-threshold (Opsi 3)**: complexity tingi, tidak lebih robust dari prioritized.
- **Grace window 90s (paseo)**: terlalu pendek untuk pindah device.
- **Grace window 30 menit (Codex)**: boros memory untuk single-user 9ed.

## Konsekuensi

- `SessionManager` extend: grace window timer per SessionConnection (clientId), configurable via env. Coordination dengan ADR-0006 SessionConnection map.
- `chatStream` extend: dua channel per subscriber, routing logic per event type, drop-oldest untuk bulk, Cancel+done untuk priority overflow.
- Client-side: seq gap detection (ADR-0002) untuk bulk catch-up, UI handling untuk `stopReason: "client_backpressure"`.
- Effort: ~3 hari (grace window 1 hari + prioritized channel 2 hari).
