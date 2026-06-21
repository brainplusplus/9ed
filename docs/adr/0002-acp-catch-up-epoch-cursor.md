# ADR-0002: ACP catch-up via epoch + cursor + replay-on-subscribe

**Status**: accepted
**Date**: 2026-06-21

ACP event stream multi-client catch-up memakai pola Paseo: **epoch (UUID, berubah saat session resume/reset) + monotonic seq + cursor-based fetch RPC + replay-on-subscribe**. Menutup gap G1 (client B connect mid-turn tidak lihat history).

## Konteks

9ed sudah punya `chat_events` table dengan `seq` monotonic + index `idx_events_session_seq` (`store.go:171,181`). Tapi tidak ada epoch, tidak ada cursor-based query (after/before seq), tidak ada replay-on-subscribe. Client B connect mid-turn hanya dapat event baru dari `chatStream` subscriber; harus manual fetch `/api/chat/history` (full load `ORDER BY seq ASC`). Tidak ada stale cursor handling untuk session resume.

## Keputusan

1. **Tambah `epoch` column** di `chat_events` (TEXT, UUID). Generate saat session create. Regenerate saat session resume (ACP subprocess mati + spawn ulang via `session/resume`).
2. **Cursor-based query**: `WHERE session_id=? AND seq > ? ORDER BY seq ASC LIMIT ?`. Reuse table yang sudah ada.
3. **`fetchTimeline(sessionID, cursor, direction, limit)` via WS** (bukan REST, agar unified dengan live stream). Response: `{epoch, reset, staleCursor, gap, window:{minSeq,maxSeq,nextSeq}, hasOlder, hasNewer, endCursor, events}`.
4. **Replay-on-subscribe**: saat client subscribe ke `chatStream`, kirim last N events (default 50, configurable) + current epoch + nextSeq.
5. **Stale cursor handling**: client fetch dengan cursor.epoch ≠ current epoch → response `staleCursor: true, reset: true` → client re-fetch tail.
6. **Gap detection**: client fetch `after` dengan cursor.seq < minSeq - 1 → response `gap: true, reset: true` → client re-fetch tail.
7. **Client-side**: track `{epoch, lastSeq}`. Reconnect = fetch `after` cursor. Epoch mismatch = re-fetch tail.

Pattern source: Paseo `InMemoryAgentTimelineStore` (`agent-timeline-store.ts:138-295`), `fetch_agent_timeline_request` (`session.ts:8292-8390`), stale/gap handling (`agent-timeline-store.ts:239-251`).

## Rejected alternatives

- **Replay-on-subscribe saja (no epoch, no cursor RPC)**: tidak menangani session resume (epoch baru = agent history baru, client tidak detect). Race condition disconnect-reconnect duplikat.
- **Cursor-based fetch via existing REST (no epoch)**: gap antara fetch history dan subscribe WS (race condition: event terjadi di antara).

## Konsekuensi

- Schema migration: add `epoch` column to `chat_events`. Backward-compatible (nullable, backfill dengan default epoch untuk existing rows).
- WS handler baru untuk `fetchTimeline` RPC.
- `chatStream.Subscribe()` extend: replay last N + epoch + nextSeq.
- Client-side cursor tracking + stale/gap handling.
- Effort: ~3-4 hari.
