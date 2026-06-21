# Multi-Client Landscape Research

**Date**: 2026-06-21
**Type**: Landscape research (pre-decision). Bukan ADR keputusan.
**Status**: Reference document, companion to `docs/adr/0000-acp-multi-client-research.md`.

Riset mendalam atas 9+ implementasi dunia nyata yang relevan dengan masalah 9ed: koneksi AI agent + multi-client concurrent sync, untuk ACP dan PTY mode, future-proof untuk SSH. Dokumen ini memberi evidence base untuk keputusan grilling.

---

## Ringkasan eksekutif

Lanskap terbagi tiga kategori, masing-masing dengan pelajaran berbeda:

1. **Web terminal sharing** (ttyd, gotty, sshx, tmate): masalah PTY multi-client dengan input control. **sshx adalah state-of-the-art** (CRDT, E2E, predictive echo ala Mosh, reconnect). **ttyd adalah baseline sederhana** (readonly by default, broadcast output, `-W` untuk writable).
2. **Agent server / daemon** (Codex App Server, opencode-multiplexer, paseo, tycho): masalah ACP-like multi-client dengan persistent session. **Codex App Server adalah yang paling matang** (Thread/Turn/Item primitives, thread/unsubscribe dengan unload 30 menit, bounded queue backpressure, experimental capability opt-in). **opencode-multiplexer menunjukkan pola refactor** dari per-instance Bus ke global map keyed by projectID.
3. **Collaborative editor / remote dev** (Zed CRDT, JupyterLab Yjs, code-server, Daytona, DevPod): sebagian overkill (CRDT), sebagian tidak support multi-client same session (code-server).

**Counter-example penting**: code-server secara eksplisit TIDAK support concurrent multi-client same session (setiap user = instance terpisah). Ini validasi bahwa target 9ed (concurrent multi-client) bukan trivial dan tidak semua player menyelesaikannya.

---

## 1. ttyd — Web Terminal Sharing (Baseline Sederhana)

**Stack**: C, libwebsockets, libuv. Single binary.
**Repo**: github.com/tsl0922/ttyd

### 1.1 Arsitektur

- 1 PTY subprocess, multiple WebSocket client connect ke endpoint `/ws`.
- `callback_tty` (libwebsockets protocol handler) mengelola per-client state (`struct pss_tty`).
- Output PTY di-broadcast ke semua connected client.
- Ping interval 5 detik (default), hangup setelah ping timeout.

### 1.2 Input control — pola paling sederhana di lanskap

ttyd punya **readonly by default**. Flag `-W` / `--writable` mengaktifkan input dari client.

- **Readonly mode** (default): semua client hanya lihat output. Tidak ada input dari client manapun. Cocok untuk "share terminal untuk demonstrasi".
- **Writable mode** (`-W`): SEMUA client bisa ngetik. Tidak ada arbitration. Input dari semua client di-merge ke satu PTY. Chaos untuk multi-user aktif, tapi acceptable untuk "1 orang utama + observer偶尔 ikut ngetik".

Tidak ada primary/secondary, tidak ada lock, tidak ada request control. Pola biner: readonly atau free-for-all.

### 1.3 Resilience

- LWS retry policy: backoff `[1000, 2000, 3000, 4000, 5000]` ms, `secs_since_valid_ping=5`, `secs_since_valid_hangup=10`.
- `-m max-clients`: batas jumlah client.
- `-o once`: accept 1 client, exit on disconnect.
- `-q exit-no-conn`: exit saat semua client disconnect.

### 1.4 Pelajaran untuk 9ed

- **Readonly default adalah pilihan yang aman**. ttyd membuktikan "terminal sharing" lebih sering read-only (demo, monitoring) daripada collaborative input. 9ed bisa default ke viewer mode untuk PTY multi-client.
- **Broadcast output = trivial**. ttyd hanya broadcast byte stream PTY ke semua socket. Tidak ada snapshot, tidak ada catch-up cursor. Client baru mulai dari "sekarang". Ini kelemahan untuk 9ed (client B miss history), tapi menunjukkan baseline minimal.
- **Input arbitration biner (readonly/writable) cukup untuk MVP**. ttyd tidak butuh primary/secondary untuk jadi produk yang berguna. 9ed bisa mulai dari sini sebelum evolve ke request control.

---

## 2. sshx — Collaborative Terminal (State-of-the-Art untuk PTY Multi-Client)

**Stack**: Rust (server + client), SvelteKit + TypeScript (web frontend), Redis (snapshot/recovery), Fly.io (global mesh).
**Repo**: github.com/ekzhang/sshx (7.5k stars)

### 2.1 Arsitektur

```
sshx client (PTY host) ──gRPC──► sshx server (Fly.io mesh)
                                    │
                                    ├── Redis (snapshot, recovery, 32KB shell data)
                                    │
Browser client (SvelteKit) ◄──WebSocket──► sshx server
```

- **Client** = mesin yang punya PTY (host terminal). Spawn PTY, stream output terenkripsi ke server.
- **Server** = relay + coordinator. Globally distributed mesh (Fly.io multi-region). Tidak bisa baca konten (E2E encrypted).
- **Browser** = collaborative frontend. Multiple browser client connect ke server, lihat terminal yang sama, dengan cursor real-time, chat, infinite canvas (multiple terminal windows).
- **Redis** = snapshot storage untuk server recovery + inactive expiry. Shell data di-snapshot 32KB per session.

### 2.2 Multi-client sync — CRDT terminal

sshx menggunakan **terminal state synchronization** (terinspirasi Mosh SSP), bukan raw byte stream:

- Setiap client maintain **replica terminal state** (character-cell grid).
- Server adalah **relay + snapshot point**, bukan source of truth untuk terminal content (content E2E encrypted, server tidak bisa baca).
- Terminal output = **state diff** (bukan byte stream). Client apply diff ke replica lokal.
- **Late-joiner**: client baru request snapshot dari server (32KB terkompresi), lalu apply diff berikutnya. Tidak replay dari awal.
- **Reconnect**: client punya state lokal, reconnect hanya butuh diff sejak disconnect. 60s reconnect window.

### 2.3 Input control — collaborative (semua bisa ngetik)

sshx **memungkinkan semua client ngetik bersamaan**. Tidak ada primary/secondary lock. Kunci pembeda:

- **Predictive echo ala Mosh**: client menampilkan keystroke lokal secara spekulatif sebelum server konfirmasi. Jika prediksi benar (normal typing, backspace, arrow keys), tidak ada lag. Jika salah, client rollback ke state server.
- **Cursor real-time**: setiap client punya cursor yang terlihat oleh client lain (seperti Google Docs cursor). Input tidak konflik karena user lihat cursor orang lain.
- **Conflict resolution**: terminal adalah last-write-wins per cell. Tidak ada operasi concurrent yang ambigu (berbeda dari text editor CRDT).

### 2.4 Resilience

- **60-second client reconnect** (PR #80). Client reconnect otomatis dalam 60s, state di-preserve.
- **Server recovery + peer routing** (PR #15). Redis snapshot, server discovery, peer routing. Jika satu server down, session di-migrate ke server lain via snapshot.
- **E2E encryption**: Argon2 (key derivation) + AES (content encryption). Server zero-knowledge.
- **Predictive echo** = resilience terhadap latency. User tidak merasa lag bahkan pada koneksi 200ms+.

### 2.5 Pelajaran untuk 9ed

- **State synchronization > byte stream** untuk PTY multi-client. sshx membuktikan replica terminal state + diff lebih robust daripada broadcast byte mentah. Client B join mid-session = snapshot + diff, bukan "miss everything sebelumnya".
- **Predictive echo adalah game-changer untuk mobile/remote**. Tapi effort tinggi: butuh terminal emulator state machine di client (xterm.js sudah punya, tapi integrasi predictive echo non-trivial). Defer ke Phase 2+.
- **Collaborative input (semua ngetik) viable dengan cursor visibility**. Tapi untuk 9ed, ini mungkin overkill — developer workflow biasanya 1 orang ngetik, lainnya monitor. Primary/secondary (ttyd-style upgrade) lebih sesuai use case.
- **Redis snapshot/recovery**: terlalu kompleks untuk 9ed single-binary. Tapi konsep "snapshot untuk late-joiner" bisa di-adaptasi ke in-memory ring buffer (paseo terminal-restore pattern) tanpa Redis.
- **E2E encryption**: relevan jika 9ed build application-level relay (paseo-relay pattern). Untuk tunnel cloudflared/bore, TLS sudah cukup.

---

## 3. Codex App Server — Agent Server Protocol (Matang untuk ACP-Like Multi-Client)

**Stack**: Rust. Subprocess via stdio/unix-socket/websocket.
**Repo**: github.com/openai/codex (`codex-rs/app-server/`)

### 3.1 Arsitektur — Thread/Turn/Item primitives

Tiga primitive top-level:

- **Thread**: satu percakapan user ↔ agent. Berisi multiple Turn. Persisten, bisa di-resume. Setara dengan "ChatSession" di 9ed.
- **Turn**: satu siklus user input → agent response. Berisi multiple Item. Setara dengan "Turn" di 9ed CONTEXT.md.
- **Item**: unit input/output (user message, agent reasoning, agent message, shell command, file edit). Persisten, dipakai sebagai context untuk conversation selanjutnya. Setara dengan "Chat Event" di 9ed.

### 3.2 Multi-client — thread subscription model

- **`thread/start`**, **`thread/resume`**, **`thread/fork`**: auto-subscribe connection ke turn/item events untuk thread tersebut.
- **`thread/unsubscribe`**: unsubscribe connection dari thread events. **Jika ini subscriber terakhir, server keep thread loaded selama 30 menit tanpa subscriber + tanpa activity, baru unload** + emit `thread/closed`. Ini grace window yang lebih panjang dari paseo (90s) — 30 menit.
- **`thread/loaded/list`**: list thread yang currently loaded in memory.
- **Multiple connection subscribe ke thread yang sama**: semua connection terima `item/*` dan `turn/*` notifications. Fan-out by construction.

### 3.3 Protocol evolution — experimental capability opt-in

- `initialize` handshake per connection. Client deklarasi `capabilities.experimentalApi: true` untuk akses experimental methods/fields.
- Tanpa opt-in, experimental methods ditolak dengan error `<descriptor> requires experimentalApi capability`.
- Field-level gating: `#[experimental("thread/start.myField")]` annotation. Enum variant gating juga (`#[experimental("askForApproval.granular")]`).
- Schema generation: `generate-ts` / `generate-json-schema`, dengan `--experimental` flag.

### 3.4 Backpressure — bounded queue

- Server pakai **bounded queues** antara transport ingress, request processing, outbound writes.
- Saat ingress saturated: reject dengan JSON-RPC error code `-32001`, message `"Server overloaded; retry later."`.
- Client harus treat sebagai retryable, exponential backoff with jitter.
- Ini berbeda dari 9ed yang drop subscriber diam-diam (G2 di doc 0000).

### 3.5 Mid-execution interaction

- **`turn/steer`**: add user input ke in-flight turn TANPA start turn baru. User bisa "mengarahkan" agent yang sedang bekerja. 9ed belum punya ini.
- **`turn/interrupt`**: cancel in-flight turn.
- **`thread/inject_items`**: append raw items ke thread history tanpa start turn.

### 3.6 Pelajaran untuk 9ed

- **Thread/Turn/Item naming lebih jelas dari ChatSession/ChatEvent.** Codex membedakan "conversation container" (Thread), "interaction cycle" (Turn), dan "atomic unit" (Item) secara eksplisit. 9ed saat ini menggabung Thread+Turn di "ChatSession" dan Item di "ChatEvent". Pertimbangkan rename untuk clarity (tapi ini ADR-eligible: hard to reverse, surprising).
- **30-minute unload grace** lebih panjang dari paseo (90s). Pilihan trade-off: resource memory vs reconnect UX. Untuk 9ed single-user, 90s-5menit mungkin cukup.
- **Bounded queue dengan explicit error (-32001)** > silent subscriber drop. Client tahu kapan retry. 9ed harus ganti `default: delete(sub)` menjadi backpressure signal.
- **`turn/steer` (mid-execution injection)** adalah feature high-value yang 9ed belum punya. User bisa koreksi agent yang sedang jalan tanpa cancel. Tapi ini butuh agent support (ACP `session/prompt` saat turn aktif? atau message queue).
- **Experimental capability opt-in** adalah pola protocol evolution yang clean. 9ed bisa adopsi untuk WS message shape evolution. Tapi mungkin overkill untuk 9ed yang single-deployer (tidak ada client lama di luar sana).
- **`thread/fork`** (branch conversation) menarik tapi out-of-scope untuk robustness MVP.

---

## 4. opencode-multiplexer — Refactor Pattern (Bus Global Map)

**Stack**: TypeScript (fork of opencode).
**Repo**: github.com/millerjes37/opencode-multiplexer

### 4.1 Arsitektur — refactor dari per-Instance ke global map

Kunci perubahan dari opencode upstream:

- **Bus system refactored** dari per-Instance ke **global map keyed by projectID**. Sebelumnya: setiap Instance punya Bus sendiri. Sesudah: satu global Bus map, event di-scope by projectID.
- **SSE event filtering by projectID**: client hanya terima event untuk project yang relevan. Mencegah cross-project event leakage.
- **Connection tracking**: setiap connection punya unique client ID. `/status` endpoint menunjukkan connected clients + per-project metrics. `/health` untuk load balancer.

### 4.2 Auth

- Token-based authentication: `generateToken`, `hashToken` (SHA-256), `validateToken`.
- `--require-auth` flag (opt-in). Bearer token.
- Token management CLI: `create`, `list`, `revoke`.
- Backward compatible: auth disabled by default.

### 4.3 Backward compatibility

- Default behavior unchanged: masih auto-spawn server jika `--server` tidak diberikan.
- `--server` flag optional (opt-in untuk multi-client mode).
- Existing single-client setup bekerja identik.

### 4.4 Pelajaran untuk 9ed

- **Global Bus keyed by session ID** = pola yang 9ed sudah punya (`chatStreamRegistry` keyed by sessionID). opencode-multiplexer membuktikan pola ini valid untuk multi-client.
- **Event filtering by scope** penting. 9ed saat ini semua chatStream independen (tidak ada cross-session leakage karena sudah keyed by sessionID). Tapi kalau nanti ada "workspace-level events", perlu scoping.
- **Connection tracking + `/status` endpoint** berguna untuk debugging multi-client. 9ed bisa tambahkan untuk observability.
- **Token auth**: 9ed sudah punya basic auth (`BASIC_AUTH_USERNAME`/`PASSWORD`). Untuk multi-client dengan identitas berbeda, perlu token per-client. Tapi untuk MVP, basic auth shared cukup.
- **Backward compatibility approach** (opt-in flag) bagus, tapi 9ed sebagai single-binary dengan single deployer bisa lebih agresif: multi-client on by default.

---

## 5. Mosh — State Synchronization Protocol (Teoretical Foundation untuk PTY Resilience)

**Stack**: C++/Perl. UDP-based.
**Paper**: "Mosh: An Interactive Remote Shell for Mobile Clients" (USENIX ATC 2012)

### 5.1 SSP (State Synchronization Protocol)

Mosh tidak mengirim byte stream. Mosh meng-synchronize **state terminal emulator** (character-cell grid) antara client dan server:

- Client dan server masing-masing maintain **replica terminal emulator state**.
- Server adalah authority. Client mengirim input, server apply + kirim state diff balik.
- **State diff = per-cell changes**, bukan byte sequence. Lebih efisien untuk roaming + intermittent connectivity.

### 5.2 Predictive echo

- Client **memprediksi** hasil keystroke lokal secara spekulatif (normal typing, backspace, left/right arrow).
- Jika prediksi confident, client tampilkan tanpa menunggu server. **Zero-lag typing**.
- Jika prediksi salah (server state berbeda), client **rollback** ke state server.
- Model prediksi adaptif: belajar dari pola typing.

### 5.3 Roaming

- Client bisa pindah IP address (switch WiFi → cellular) tanpa disconnect.
- Session di-identify oleh session ID, bukan IP. Client reconnect ke session ID, server resume state.
- UDP-based, no TCP connection state.

### 5.4 Pelajaran untuk 9ed

- **State sync > byte stream untuk unreliable network.** Mosh (2012) dan sshx (2023) keduanya konvergen ke pola ini. Tapi effort tinggi: butuh terminal emulator state machine.
- **Predictive echo = killer feature untuk mobile**, tapi kompleks. Defer ke Phase 3+ 9ed.
- **Roaming by session ID, bukan IP**: 9ed WS sudah session-based (sessionID di URL), jadi ini gratis selama grace window ada.
- **Mosh bukan multi-client** (1:1 client-server). Tapi pola state sync-nya adalah foundation sshx untuk multi-client.

---

## 6. tmux / tmate — Client-Server Terminal Multiplexer (Battle-Tested Baseline)

**Stack**: C. Unix socket IPC.

### 6.1 Arsitektur

- **tmux server**: 1 process, persistent. Owns session, window, pane.
- **tmux client**: connect ke server via unix socket. Multiple client bisa attach ke session yang sama.
- **Session**: persistent. Survive client disconnect. `tmux attach` untuk reconnect.
- **tmate**: fork tmux dengan SSH-based remote sharing. Session di-share via SSH connection ke tmate server.

### 6.2 Multi-client

- **Multiple client attach same session**: semua lihat output yang sama. Input dari semua client di-merge (free-for-all, seperti ttyd `-W`).
- **No input arbitration**: siapa ngetik, masuk. Tidak ada lock, tidak ada primary.
- **`readonly` mode**: tmux support `readonly` per-client (`:clientlist` + `:resize-pane -Z`). Tapi bukan fitur utama.
- **Window-level isolation**: client bisa attach ke window berbeda dalam session yang sama. Tapi ini window multiplexing, bukan concurrent same-window collaboration.

### 6.3 Pelajaran untuk 9ed

- **Persistent session = core value.** tmux/tmate eksis selama 15+ tahun karena session survive disconnect. 9ed harus pastikan ChatSession survive WS disconnect (grace window + lazy unload).
- **Free-for-all input viable untuk technical user.** tmux user accept chaos input karena mereka tahu apa yang mereka lakukan. Tapi untuk 9ed yang target developer workflow, primary/secondary lebih aman.
- **Client-server via unix socket**: 9ed sudah WS-based, bukan unix socket. Tapi konsep "server persistent, client ephemeral" sama.

---

## 7. JupyterLab RTC — CRDT Collaborative Editing (Referensi untuk Editor, Bukan Terminal)

**Stack**: Python (jupyter-server), Yjs (CRDT), WebSocket.
**Repo**: github.com/jupyterlab/jupyter-collaboration

### 7.1 Arsitektur

- **Yjs CRDT** untuk collaborative document editing. Setiap client punya Yjs document replica.
- **jupyter-server** sebagai relay + awareness. Tidak bisa baca content jika encrypted (tapi default tidak encrypted).
- **Awareness protocol**: client tahu siapa online, cursor position, selection.
- **Late-join**: client baru request document state, apply Yjs update, lalu stream real-time updates.

### 7.2 Pelajaran untuk 9ed

- **CRDT untuk text editing, bukan terminal.** 9ed tidak collaborative text editor (Monaco editor di 9ed = single-user). Yjs overkill.
- **Awareness protocol (who's online, cursor)**: menarik untuk 9ed chat multi-client ("client A sedang mengetik..."). Tapi low priority.
- **JupyterHub (multi-user server)**: setiap user = Jupyter server terpisah. Bukan multi-client same session. Counter-example seperti code-server.

---

## 8. code-server — Counter-Example (TIDAK Support Multi-Client Same Session)

**Stack**: TypeScript (VS Code fork), Node.js.
**Repo**: github.com/coder/code-server

### 8.1 Arsitektur

- code-server = VS Code di browser. 1 instance = 1 user.
- **TIDAK support concurrent multi-client same session.** Setiap user butuh instance terpisah.
- Issue #33 (Collaboration support, 2019): masih terbuka. Code依赖 VS Code Live Share extension untuk collaboration, bukan native.
- Diskusi #2634 (Concurrent connections): rekomendasi = multiple instance atau Live Share.

### 8.2 Pelajaran untuk 9ed

- **Validasi bahwa multi-client same session bukan trivial.** code-server (backed by Coder, well-funded) eksplisit tidak menyelesaikannya secara native. Mereka rely pada extension (Live Share) yang terpisah.
- 9ed menargetkan sesuatu yang code-server tidak lakukan. Ini diferensiasi, tapi juga berarti tidak ada "standar industri" yang langsung bisa di-copy. Paseo + sshx + Codex App Server adalah referensi terdekat.

---

## 9. Daytona / DevPod — Remote Dev Environment Manager (Context, Bukan Multi-Client Sync)

**Stack**: Go (Daytona), Go (DevPod).

### 9.1 Arsitektur

- **Daytona**: provision dev environment (container/VM), bukan multi-client sync. 1 environment = 1 user. Fokus pada "spin up environment cepat" (90ms), bukan "multiple client access same environment".
- **DevPod**: similar. Manage dev environment across providers (Docker, SSH, Kubernetes). Single-client per environment.

### 9.2 Pelajaran untuk 9ed

- **Tidak ada pelajaran multi-client langsung.** Daytona/DevPod selesaikan masalah berbeda (environment provisioning).
- **Relevansi untuk SSH masa depan 9ed**: jika 9ed nanti support "agent berjalan di remote machine via SSH", pola Daytona/DevPod (provider abstraction, environment lifecycle) relevan. Tapi itu cabang design tree tersendiri, defer.

---

## Sintesis — Evidence-Based Rekomendasi per Pertanyaan Grilling

### Untuk Q3 (PTY/SSH input arbitration)

Evidence dari lanskap:

| Implementasi | Input Control | Trade-off |
|-------------|---------------|-----------|
| ttyd | Readonly default, `-W` free-for-all | Sederhana, biner. Chaos jika writable + multi-user aktif |
| sshx | Free-for-all + predictive echo + cursor | State-of-the-art, tapi effort tinggi. Butuh terminal state machine |
| tmux/tmate | Free-for-all | Battle-tested, technical user accept chaos |
| Codex App Server | N/A (ACP-like, input = prompt terstruktur) | Tidak ada input conflict |

**Rekomendasi berdasarkan evidence**: **ttyd-style upgrade path** untuk 9ed:
1. **Phase 1 (MVP)**: Readonly default untuk PTY multi-client. Client B = viewer. Implementasi trivial (cek flag saat inbound WS message).
2. **Phase 2**: Primary/secondary dengan request control (Opsi A dari grilling). Satu primary bisa ngetik, viewer request control.
3. **Phase 3 (optional)**: sshx-style collaborative dengan cursor + predictive echo. High effort, high value untuk mobile.

ttyd membuktikan Phase 1 (readonly default) sudah produk yang berguna. 9ed tidak perlu langsung ke collaborative.

### Untuk Q2 (concurrent vs sequential) — sudah dijawab "concurrent"

Evidence mendukung: paseo, sshx, Codex App Server semua concurrent. code-server (sequential-only) adalah counter-example yang diferensiasi.

### Untuk future-proof SSH

Evidence: sshx (SSH-like dengan CRDT), Mosh (SSH alternative dengan SSP), Codex App Server (unix-socket transport). Pola umum: **transport-agnostic session layer**. 9ed's `ChatSession` interface sudah transport-agnostic (ACP + PTY). SSH tinggal jadi implementasi ketiga.

### Untuk backpressure (menutup G2 di doc 0000)

Evidence: Codex App Server (bounded queue + `-32001` error), paseo (dual-threshold bufferedAmount). 9ed harus ganti silent drop menjadi explicit backpressure signal.

### Untuk grace window (menutup G3)

Evidence: paseo (90s), Codex App Server (30 menit), sshx (60s reconnect), ttyd (no grace, exit on disconnect). Rentang 90s-5menit adalah sweet spot untuk 9ed single-user.

### Untuk protocol evolution

Evidence: Codex App Server (experimental capability opt-in), paseo (capability handshake + COMPAT tags). 9ed bisa adopsi pola sederhana: capability field di hello message. Tapi low priority untuk single-deployer.
