import { describe, expect, it } from 'vitest';
import {
  MAX_RECONNECT_ATTEMPTS,
  RECONNECT_BASE_DELAY_MS,
  RECONNECT_MAX_DELAY_MS,
  reconnectDelay,
} from './reconnect';

describe('reconnect backoff', () => {
  it('uses 150ms base delay matching backend RECONNECT_BASE_DELAY', () => {
    expect(RECONNECT_BASE_DELAY_MS).toBe(150);
  });

  it('uses 30s max delay matching backend RECONNECT_MAX_DELAY', () => {
    expect(RECONNECT_MAX_DELAY_MS).toBe(30_000);
  });

  it('returns base delay on first attempt (attempt 0)', () => {
    expect(reconnectDelay(0)).toBe(150);
  });

  it('doubles delay on each subsequent attempt (exponential backoff)', () => {
    expect(reconnectDelay(0)).toBe(150);       // 150 * 2^0
    expect(reconnectDelay(1)).toBe(300);       // 150 * 2^1
    expect(reconnectDelay(2)).toBe(600);       // 150 * 2^2
    expect(reconnectDelay(3)).toBe(1200);      // 150 * 2^3
    expect(reconnectDelay(4)).toBe(2400);      // 150 * 2^4
    expect(reconnectDelay(5)).toBe(4800);      // 150 * 2^5
  });

  it('caps delay at RECONNECT_MAX_DELAY_MS', () => {
    // 150 * 2^8 = 38400 > 30000, should be capped
    expect(reconnectDelay(8)).toBe(30_000);
    expect(reconnectDelay(10)).toBe(30_000);
    expect(reconnectDelay(50)).toBe(30_000);
  });

  it('allows at least 20 reconnect attempts before giving up', () => {
    expect(MAX_RECONNECT_ATTEMPTS).toBeGreaterThanOrEqual(20);
  });
});
