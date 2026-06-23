// ADR-0006: Exponential backoff for WebSocket reconnection.
//
// These defaults mirror the backend config values RECONNECT_BASE_DELAY (150ms)
// and RECONNECT_MAX_DELAY (30s) defined in internal/config/config.go. They are
// client-side per the ADR — the backend documents them in .env.example for
// client config parity.

export const RECONNECT_BASE_DELAY_MS = 150;
export const RECONNECT_MAX_DELAY_MS = 30_000;

// With exponential backoff capped at 30s, 20 attempts covers ~5 minutes of
// reconnection tries before giving up.
export const MAX_RECONNECT_ATTEMPTS = 20;

/**
 * Compute the reconnect delay for a given attempt number using exponential
 * backoff: delay = base * 2^attempt, capped at max.
 *
 * @param attempt - zero-based attempt index (0 = first retry)
 * @returns delay in milliseconds
 */
export function reconnectDelay(attempt: number): number {
  const delay = RECONNECT_BASE_DELAY_MS * Math.pow(2, attempt);
  return Math.min(delay, RECONNECT_MAX_DELAY_MS);
}
