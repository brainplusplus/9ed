# ADR-0001: Unified pion transport + pluggable visual stream strategy

**Status**: accepted
**Date**: 2026-06-21
**Supersedes**: nothing (this is a revision of the earlier H264-only draft)

Semua surface visual streaming 9ed (collaborative browser, remote desktop masa depan, collaborative terminal/SSH berbasis sshx-style) menggunakan **satu transport terpadu (pion/webrtc)** dengan **frame source dan visual stream strategy yang pluggable**. Browser collaborative memakai JPEG tile diff (pola 9remote). Remote desktop memakai JPEG tile diff atau H264 full frame (user konfigurasi). Tidak ada CGo. Tidak ada transport divergence.

## Konteks

Riset lanskap (`docs/adr/0000-acp-multi-client-research.md` + `docs/research/multi-client-landscape.md`) mengevaluasi tiga pola transport. Target 9ed adalah real (bukan MVP) + future-proof untuk remote desktop dan collaborative terminal/SSH. Bongkar 9remote (`references/` di luar repo, package npm `9remote@2.0.22`) mengungkap pola production-ready: **JPEG tile diff + WebRTC DataChannel** untuk remote desktop dengan klaim <50ms latency, klik kan dan touch Ancodebuddy berfungsi, tanpa H264. 9remote memakai `sharp` (JPEG), `node-datachannel` (WebRTC), `node-screenshots`/`robotjs` (capture + input), tile size 128-256px, adaptive quality tiers.

Strategi transport divergence (WS untuk browser, pion untuk remote desktop) ditolak karena menambah dua stack transport. Strategi H264-only untuk semua surface ditolak setelah riset 9remote karena overkill untuk screen content mostly-static (browser collaborative, desktop coding) dan menambah kompleksitas (ffmpeg subprocess, NAL parser, auto-download) yang tidak proporsional dengan benefit untuk use case dominan 9ed.

## Keputusan

**Satu transport (pion/webrtc) + dua pluggable layer:**

### Layer 1: Frame Source (pluggable)

Interface yang produce raw frame pixels. Implementasi per-surface:
- **Browser collaborative**: CDP `Page.startScreencast` via Playwright `NewCDPSession` (`page.Context().NewCDPSession(page)`). Verified di playwright-go v0.5700.1 (`generated-interfaces.go:373`). Produce JPEG/PNG frame bytes via CDP event.
- **Remote desktop (future)**: Screen capture native (DXGI Desktop Duplication di Windows, X11/PipeWire di Linux, ScreenCaptureKit di macOS).

### Layer 2: Visual Stream Strategy (pluggable)

Interface yang encode frame dan distribusi ke subscriber via pion. Dua implementasi:

**Strategy A: JPEG tile diff (default untuk browser collaborative, option untuk remote desktop)**
- Encoder: `image/jpeg` stdlib (pure Go, no dependency)
- Rendering: tile-based diff. Split frame jadi tiles 128-256px (config per-platform). Kirim HANYA tiles yang berubah via pion DataChannel (binary, unordered, unreliable untuk frame terbaru prioritas).
- Pola: replikasi 9remote (`TileManager` + `ScreenHandler` + DataChannel `dcMaxTilesPerFrame`, `dcChunkSize`, `dcOrdered: false`).
- Adaptive quality: tiers berdasarkan effectiveness (9remote: quality 45-72, outputScale 0.65-1.0).
- Pro: pure Go, no external dependency, lower latency (encode only changed tiles), excellent untuk mostly-static screen content.
- Kontra: poor untuk full motion (many tiles change = bandwidth tinggi).

**Strategy B: H264 full frame (option untuk remote desktop, untuk full motion use case)**
- Encoder: ffmpeg subprocess (auto-download binary via `installBinary` pattern dari `internal/tunnel/tunnel.go`), pipe stdin (raw frame) → stdout (H264 NAL units).
- Rendering: full frame H264 encode, kirim via pion video track (`TrackLocalStaticSample`).
- Pro: excellent untuk full motion (inter-frame compression), universal codec, hardware decode di client `<video>` element.
- Kontra: external dependency (ffmpeg binary), encode latency (10-50ms), NAL unit parser, subprocess management. Overkill untuk static content.

**Default per-surface:**
- Browser collaborative → Strategy A (JPEG tile diff). Browser mostly static, pure Go path, 9remote bukti production.
- Remote desktop → user konfigurasi. Default Strategy A (sufficient untuk coding, 9remote bukti). Upgrade ke Strategy B jika full motion (video editing, gaming) needed.

### Layer 3: Input handling (unified, independent dari codec)

Input handling TIDAK terikat ke visual stream strategy. Codec hanya soal visual frame. Klik kan, touch, gesture = input layer yang berbeda. **Catatan transport**: input untuk visual surfaces (browser, remote desktop) pakai pion DataChannel. Input untuk PTY/ACP (chat terminal) pakai WS. ADR-0005 mendefinisikan input handling PTY secara detail (soft lock, cursor overlay via WS).
- Transport input (browser/remote desktop): pion DataChannel (low latency, UDP).
- Transport input (PTY/chat): WS (konsisten dengan ADR-0005, ADR-0006).
- Client-side gesture mapping (frontend React): long press → right-click, two-finger tap → right-click, single tap → left-click, two-finger drag → scroll, pinch → zoom. Pola 9remote untuk Ancodebuddy compatibility (klik kan berfungsi).
- Server-side input injection: platform-specific. Windows = SendInput API, Linux = XTest/uinput, macOS = CGEventPost. Throttle (9remote: mouse 8ms, key 25ms, type text 100ms) untuk responsiveness.
- Input arbitration untuk collaborative (multi-client): collaborative + soft lock (dari ADR-0005). Semua client bisa input; soft lock 2 detik cegah garbled untuk PTY. ACP mode = collaborative default (no lock, turn-based queue). Cursor overlay untuk semua client.

## Alasan dual strategy (bukan single)

1. **Use case karakter berbeda.** Browser collaborative = mostly static (web pages, forms, text). Remote desktop = bervariasi (static coding vs full motion video/gaming). Single strategy suboptimal untuk salah satu.
2. **Transport tetap unified.** pion/webrtc untuk semua. Yang pluggable = strategy (encoder + rendering + channel). Tidak ada divergence transport stack.
3. **9remote bukti JPEG tile diff production-ready** untuk remote desktop developer tool (<50ms latency, klik kan + touch Ancodebuddy). Strategy A viable untuk remote desktop, Strategy B adalah upgrade option, bukan keharusan.
4. **Pure Go path untuk browser.** Strategy A = `image/jpeg` stdlib + pion. No external dependency, no subprocess, no auto-download. Mempertahankan UX install 9ed (1 binary).
5. **H264 path untuk full motion** (Strategy B) tetap tersedia sebagai option untuk remote desktop, tanpa memaksa browser collaborative ikut kompleks.

## Konsekuensi

- **Dua codepath strategy untuk maintain**: JPEG tile diff logic (tile manager, diff, DataChannel packaging) + H264 pipeline (ffmpeg subprocess, NAL parser, video track). Mitigasi: abstraction layer clean, strategy = interface, masing-masing implementasi independent.
- **Dua transport channel pattern**: pion DataChannel (untuk JPEG tiles + input) vs pion video track (untuk H264). Tapi keduanya via pion PeerConnection yang sama, hanya channel type berbeda.
- **Strategy A default, Strategy B opt-in**: user konfigurasi per-surface. Browser selalu Strategy A. Remote desktop default Strategy A, upgrade Strategy B jika needed.
- **Input handling independent**: pion DataChannel untuk input, tidak peduli visual strategy. Gesture mapping client-side + platform injection server-side. Klik kan + touch Ancodebuddy teratasi di input layer, bukan codec.
- **ffmpeg auto-download hanya jika Strategy B dipilih**. User yang pakai Strategy A (default) tidak butuh ffmpeg. Reuse `installBinary` pattern dari `internal/tunnel/tunnel.go` untuk Strategy B.

## Trade-off yang diterima

- Complexity dua strategy vs optimalitas per-use-case. Trade-off acceptable karena abstraction clean dan 9remote bukti Strategy A viable untuk remote desktop.
- Browser collaborative tidak akan pernah full motion (Strategy A saja). Trade-off acceptable; browser collaborative = developer tool, bukan video player.
- Remote desktop default Strategy A (bukan H264) = sedikit quality loss untuk static content, tapi massive simplicity gain. User bisa upgrade ke Strategy B. Trade-off acceptable.

## Upgrade path

- Strategy A → Strategy B: ganti visual stream strategy implementation. Frame source tetap. Transport (pion) tetap. Hanya encoder + rendering + channel yang berubah.
- Strategy B → Strategy A: reverse, simplify.
- Browser Strategy A → H264: technically possible tapi tidak rekomendasi (overkill).
- Tambah strategy baru (misal VP9, AV1, sshx-style CRDT): implementasi strategy baru, interface tetap.

## Rejected alternatives

- **WS+JPEG untuk browser, pion untuk remote desktop** (transport divergence): dua transport stack, upgrade path sulit. Ditolak.
- **H264-only untuk semua surface**: overkill untuk browser collaborative, complexity tidak proporsional. Ditolak setelah riset 9remote.
- **JPEG tile diff-only untuk semua surface**: poor untuk full motion remote desktop. Ditolak karena remote desktop future-proof untuk full motion.
- **CGo encoder** (go-astiav, pion x264, pion openh264): melangar prinsip pure Go 9ed, build friction, cross-compilation rusak. Ditolak.
- **ffgo purego binding**: 9 stars, 1 maintainer, supply chain risk tinggi, purego binding gotcha cross-platform. Ditolak.
- **openh264**: screen content bitrate spike issues (issue #2732), lower quality + slower dari x264, masih CGo. Ditolak.
