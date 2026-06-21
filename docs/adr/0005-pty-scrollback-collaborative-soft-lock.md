# ADR-0005: PTY scrollback (ring buffer + xterm.js snapshot) + collaborative soft lock input

**Status**: accepted
**Date**: 2026-06-21

Dua keputusan untuk PTY mode multi-client: **scrollback catch-up via adaptive strategy** (ring buffer untuk append-only output + primary-sourced xterm.js snapshot untuk TUI), dan **collaborative input dengan soft lock** (semua client bisa ngetik, soft lock 2 detik cegah garbled simultanuous typing, cursor visibility).

## Konteks

PTY mode di 9ed (`pty_session.go`) saat ini: single PTY, output push ke `events` channel (buffered 256), tidak ada scrollback replay ke client baru, tidak ada input arbitration (single client only). Multi-client PTY = konflik input + no history untuk late-joiner.

9remote (bongkar package npm): 50KB ring buffer per session, replay ke client baru, `node-datachannel` WebRTC, `robotjs` input. Pola proven untuk PTY multi-client.

xterm.js sudah dipakai 9ed di frontend (`components/terminal/`), punya `serialize` addon untuk dump terminal state.

## Keputusan

### PTY scrollback catch-up (adaptive strategy)

- **Normal output** (shell, CLI agent, ~80% use case): in-memory ring buffer (256KB-1MB, configurable via env `PTY_RING_BUFFER_SIZE=1MB`) + replay-on-subscribe. Client baru join → kirim ring buffer content → live stream setelahnya. Pola 9remote ptyDaemon.
- **TUI detection**: deteksi alternate screen buffer via byte pattern (`\033[?1049h` = TUI enter, `\033[?1049l` = TUI exit).
- **TUI mode aktif + client baru join**: snapshot request ke **primary client's xterm.js** (via WS, bukan pion — pion hanya untuk visual streaming ADR-0001). PTY multi-client pakai WS untuk scrollback + input, konsisten dengan ACP. pion DataChannel hanya untuk visual streaming (browser/remote desktop), bukan PTY. xterm.js pakai `serialize` addon → dump terminal grid state (cursor, text, colors, alternate screen buffer) → kirim ke server → relay ke client baru. Client baru apply snapshot → live stream setelahnya.
- **Edge case (primary disconnect saat TUI + client baru join)**: fallback ke ring buffer (mungkin broken state untuk TUI) atau "waiting for primary" message. Grace window (ADR-0003, 10 menit) tungu primary reconnect.

Pattern source: 9remote ptyDaemon (ring buffer + replay), xterm.js serialize addon (TUI snapshot), paseo terminal-restore (adaptive strategy concept).

### Collaborative input dengan soft lock

- **ACP mode**: collaborative default. Semua client bisa kirim prompt. Turn-based queue (agent proses satu per satu). No primary/viewer. No input conflict (prompt = terstruktur, bukan keystroke).
- **PTY mode**: collaborative + soft lock.
 - Default: semua client bisa ngetik (collaborative, sshx-style feel).
 - **Soft lock**: saat client mulai ngetik, acquire lock (2 detik TTL, auto-renew saat continue typing). Client lain yang coba ngetik → tolak + `{type:"input_locked", holder:"clientId", ttl:2s}`. UI tampilkan "X is typing...".
 - Lock auto-release on idle (2 detik no input) atau TTL expire (client crash mid-type).
 - Lock granularity: per-pane (per terminal tab), bukan per-session. Multi-pane = client A lock pane 1, B bisa ngetik pane 2.
 - **Cursor visibility**: semua client punya cursor overlay (metadata ringan: "cursor client A di baris 5 kol 10" via WS). Awareness sosial.
- **Soft lock configurable**: `PTY_INPUT_LOCK_TTL=2s` di `.env`.

## Rejected alternatives

- **Opsi 3 server-side snapshot (tmux/psmux subprocess)**: tmux tidak native Windows (WSL required). psmux alternative immature (792 stars, 1 maintainer, Maret 2026). PTY ownership transfer ke tmux = arsitektur shift. Cross-platform inconsistency.
- **Opsi 3 pure Go terminal emulator**: 6-12 bulan effort, tidak practical.
- **Opsi 3 Chromium per session**: 50-100MB per session, tidak scalable.
- **Opsi 1 ring buffer only**: broken state untuk TUI (escape sequence potong).
- **Pure collaborative (no soft lock)**: garbled input saat 2 orang ngetik simultan. Command broken.
- **Primary/viewer (ttyd-style)**: friction switch, tidak collaborative feel.

## Konsekuensi

- `ptySession` extend: ring buffer, TUI detection (`\033[?1049h`/`\033[?1049l` byte scan), snapshot broker (request ke primary, relay ke viewer).
- PTY subscriber management: multi-subscriber fan-out (reuse `chatStream` pattern tapi untuk raw bytes, bukan structured event).
- Soft lock state machine: per-pane lock, TTL, auto-renew, auto-release, client crash handling.
- Cursor overlay metadata: cursor position broadcast via WS.
- Client-side: xterm.js serialize addon integration, "X is typing..." UI, cursor overlay rendering, input lock handling.
- Env vars: `PTY_RING_BUFFER_SIZE=1MB`, `PTY_INPUT_LOCK_TTL=2s`.
- Effort: ~5-6 hari (ring buffer 1 hari, TUI detection + snapshot 2 hari, soft lock 2 hari, cursor overlay 1 hari).
