# ADR-0004: Session resume auto-restart dengan retry limit + fallback notify

**Status**: accepted
**Date**: 2026-06-21

Saat subprocess ACP mati secara tak terduga (crash, OOM, network error), 9ed **auto-restart via ACP `session/resume`** dengan bounded retry + exponential backoff + error classification. Kalau retry gagal atau agent tidak support resume, **fallback notify user** dengan actionable error. Menutup gap G5 (subprocess mati = session berakhir permanen).

## Konteks

9ed saat ini: `acp.Client.readLoop()` EOF (subprocess exit) → semua pending request di-close dengan error → `Done()` channel close → `chatStream.run()` emit `done` dengan `stopReason: "session_closed"` → stream selesai. Tidak ada auto-restart. User harus manual resume via `/api/chat/sessions/resume`.

9ed sudah punya `acp.Adapter.ResumeSession()` (`adapter.go:ResumeSession`) + capability check `SupportsResume()`. Tapi tidak dipakai untuk auto-restart.

Paseo: lazy resume on first reference (`ensureAgentLoaded`). Tycho: native `--resume` setelah subprocess mati.

## Keputusan

**State machine**: `running → crashed → restarting (retry N) → resumed | failed`.

1. **Subprocess mati** (deteksi via `acp.Client.Done()` channel close atau `Err()` non-nil).
2. **Error classification**: bedakan transient vs persistent.
 - Transient (network blip, EOF, EPIPE, OOM sesaat): retry.
 - Persistent (config error, auth expired, binary not found): langsung fallback notify, no retry.
3. **Capability check**: `SupportsResume()`. Kalau agent support resume → retry via `session/resume`. Kalau tidak support → langsung fallback notify (new session needed, history lost dari agent side, tapi 9ed `chat_events` tetap ada).
4. **Bounded retry**: max 3 retry (configurable via env `SESSION_RESUME_MAX_RETRIES=3`), exponential backoff + jitter (baseDelay 500ms, maxDelay 30s, pola paseo reconnect).
5. **Retry berhasil**: spawn ulang subprocess via `ResumeSession()`, regenerate epoch (ADR-0002 — epoch baru signal ke client bahwa timeline di-reset, client re-fetch tail), emit `{type:"session_resumed", epoch, acpSessionId}` ke subscriber, replay recent events (ADR-0002 replay-on-subscribe). User tidak sadar agent pernah mati (transparent recovery). Koordinasi dengan ADR-0002: epoch regeneration terjadi di sini (saat resume), bukan di catch-up layer.
6. **Retry gagal** (setelah max retry) atau persistent error: emit `done` dengan `stopReason: "agent_crash_unrecoverable"` + error message + `canResume: true/false`. Client tampilkan actionable UI ("Agent crashed: {error}. Click to resume" kalau canResume, atau "Start new session" kalau tidak).
7. **Grace window integration** (ADR-0003): restart terjadi dalam 10 menit grace window, subscriber tidak disconnect. Kalau restart melebihi grace window → session teardown → user manual resume.

## Rejected alternatives

- **Auto-restart transparent tanpa retry limit (Opsi 1)**: restart loop forever pada persistent crash = resource exhaustion, bisa crash 9ed sendiri.
- **No auto-restart, notify user (Opsi 3)**: UX buruk untuk transient crash (network blip kill subprocess = user harus manual resume untuk hal sepele).

## Konsekuensi

- `acp.Adapter` extend: state machine, retry logic, error classification, backoff.
- `chatStream` extend: handle `session_resumed` event, epoch regeneration, replay.
- `SessionManager` extend: integrate dengan grace window (ADR-0003), track restart state.
- Client-side: UI untuk `session_resumed` (transparent), `agent_crash_unrecoverable` (actionable error + resume button).
- Env vars: `SESSION_RESUME_MAX_RETRIES=3`, `SESSION_RESUME_BASE_DELAY=500ms`, `SESSION_RESUME_MAX_DELAY=30s`.
- Effort: ~3-4 hari.
