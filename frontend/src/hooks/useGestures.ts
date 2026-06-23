/**
 * useGestures — touch gesture recognition for the browser panel.
 *
 * Maps touch events to mouse/keyboard input events for the headless browser:
 * - Single tap → left click (mouse_down + mouse_up)
 * - Long press → right-click (button: 2)
 * - Two-finger tap → right-click (button: 2)
 * - Two-finger drag → scroll (deltaX, deltaY)
 * - Pinch → zoom (ctrl+scroll for browser zoom)
 *
 * All coordinates are normalized to 0-1 viewport space before emission.
 */
import { useCallback, useRef } from 'react';
import type { TouchEvent as ReactTouchEvent } from 'react';

export type InputEventPayload = {
  type: string;
  x?: number;
  y?: number;
  button?: number;
  key?: string;
  code?: string;
  modifiers?: number;
  text?: string;
  deltaX?: number;
  deltaY?: number;
};

type GestureCallbacks = {
  sendInput: (event: InputEventPayload) => void;
  viewportWidth: number;
  viewportHeight: number;
};

// Gesture detection thresholds.
const LONG_PRESS_MS = 500;
const TAP_MAX_DISTANCE = 15; // px — movement beyond this cancels a tap
const TWO_FINGER_TAP_MAX_DURATION = 300; // ms
const PINCH_SCALE_THRESHOLD = 0.01; // minimum scale delta to emit zoom

type TouchPoint = { x: number; y: number; t: number };

function distance(a: TouchPoint, b: TouchPoint): number {
  const dx = a.x - b.x;
  const dy = a.y - b.y;
  return Math.sqrt(dx * dx + dy * dy);
}

function midpoint(a: TouchPoint, b: TouchPoint): TouchPoint {
  return { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2, t: Math.max(a.t, b.t) };
}

/**
 * Returns touch gesture handlers to spread onto a container element.
 * The caller is responsible for attaching/detaching based on connection state.
 */
export function useGestures({ sendInput, viewportWidth, viewportHeight }: GestureCallbacks) {
  // Single-finger state.
  const singleTouchStartRef = useRef<TouchPoint | null>(null);
  const singleTouchMovedRef = useRef(false);
  const longPressTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Two-finger state.
  const twoTouchStartRef = useRef<{ a: TouchPoint; b: TouchPoint } | null>(null);
  const twoTouchMovedRef = useRef(false);
  const lastPinchDistRef = useRef<number>(0);

  const normalize = useCallback(
    (clientX: number, clientY: number, el: HTMLElement): { x: number; y: number } => {
      const rect = el.getBoundingClientRect();
      const x = Math.max(0, Math.min(1, (clientX - rect.left) / rect.width));
      const y = Math.max(0, Math.min(1, (clientY - rect.top) / rect.height));
      // Map to viewport coordinates.
      return { x: x * viewportWidth, y: y * viewportHeight };
    },
    [viewportWidth, viewportHeight],
  );

  const clearLongPressTimer = useCallback(() => {
    if (longPressTimerRef.current !== null) {
      clearTimeout(longPressTimerRef.current);
      longPressTimerRef.current = null;
    }
  }, []);

  const emitClick = useCallback(
    (x: number, y: number, button: number) => {
      sendInput({ type: 'mouse_down', x, y, button });
      sendInput({ type: 'mouse_up', x, y, button });
    },
    [sendInput],
  );

  const onTouchStart = useCallback(
    (event: ReactTouchEvent<HTMLDivElement>) => {
      const el = event.currentTarget;
      const touches = event.nativeEvent.touches;
      const now = Date.now();

      if (touches.length === 1) {
        // Single finger down — potential tap or long press.
        const touch = touches[0];
        const pt: TouchPoint = {
          ...normalize(touch.clientX, touch.clientY, el),
          t: now,
        };
        singleTouchStartRef.current = pt;
        singleTouchMovedRef.current = false;

        // Start long press timer.
        clearLongPressTimer();
        longPressTimerRef.current = setTimeout(() => {
          // If the finger hasn't moved significantly, emit right-click.
          if (singleTouchStartRef.current && !singleTouchMovedRef.current) {
            const start = singleTouchStartRef.current;
            emitClick(start.x, start.y, 2); // button 2 = right
            singleTouchStartRef.current = null;
          }
        }, LONG_PRESS_MS);
      } else if (touches.length === 2) {
        // Two fingers down — cancel single-finger gesture.
        clearLongPressTimer();
        singleTouchStartRef.current = null;

        const t0 = touches[0];
        const t1 = touches[1];
        const a: TouchPoint = {
          ...normalize(t0.clientX, t0.clientY, el),
          t: now,
        };
        const b: TouchPoint = {
          ...normalize(t1.clientX, t1.clientY, el),
          t: now,
        };
        twoTouchStartRef.current = { a, b };
        twoTouchMovedRef.current = false;
        lastPinchDistRef.current = distance(a, b);
      }
    },
    [clearLongPressTimer, emitClick, normalize],
  );

  const onTouchMove = useCallback(
    (event: ReactTouchEvent<HTMLDivElement>) => {
      const el = event.currentTarget;
      const touches = event.nativeEvent.touches;
      const now = Date.now();

      if (touches.length === 1 && singleTouchStartRef.current) {
        const touch = touches[0];
        const pt: TouchPoint = {
          ...normalize(touch.clientX, touch.clientY, el),
          t: now,
        };
        if (distance(pt, singleTouchStartRef.current) > TAP_MAX_DISTANCE) {
          singleTouchMovedRef.current = true;
          clearLongPressTimer();
        }
      } else if (touches.length === 2 && twoTouchStartRef.current) {
        const t0 = touches[0];
        const t1 = touches[1];
        const a: TouchPoint = {
          ...normalize(t0.clientX, t0.clientY, el),
          t: now,
        };
        const b: TouchPoint = {
          ...normalize(t1.clientX, t1.clientY, el),
          t: now,
        };

        const startMid = midpoint(twoTouchStartRef.current.a, twoTouchStartRef.current.b);
        const currentMid = midpoint(a, b);
        const distStart = distance(twoTouchStartRef.current.a, twoTouchStartRef.current.b);
        const distNow = distance(a, b);

        // Detect movement beyond tap threshold.
        if (
          distance(a, twoTouchStartRef.current.a) > TAP_MAX_DISTANCE ||
          distance(b, twoTouchStartRef.current.b) > TAP_MAX_DISTANCE
        ) {
          twoTouchMovedRef.current = true;
        }

        if (twoTouchMovedRef.current) {
          // Two-finger drag → scroll.
          const dx = currentMid.x - startMid.x;
          const dy = currentMid.y - startMid.y;

          // Emit scroll event. Negate to match natural scroll direction.
          sendInput({
            type: 'scroll',
            x: currentMid.x,
            y: currentMid.y,
            deltaX: -dx * 0.5,
            deltaY: -dy * 0.5,
          });

          // Update start for continuous scrolling.
          twoTouchStartRef.current = { a, b };

          // Also handle pinch: if the distance between fingers changes
          // significantly, emit a zoom (ctrl+scroll).
          const distDelta = distNow - lastPinchDistRef.current;
          const distRatio = distStart > 0 ? distDelta / distStart : 0;
          if (Math.abs(distRatio) > PINCH_SCALE_THRESHOLD) {
            sendInput({
              type: 'scroll',
              x: currentMid.x,
              y: currentMid.y,
              deltaX: 0,
              deltaY: distDelta > 0 ? -100 : 100, // zoom in = scroll up, zoom out = scroll down
              modifiers: 2, // Ctrl modifier for zoom
            });
            lastPinchDistRef.current = distNow;
          }
        }
      }
    },
    [clearLongPressTimer, normalize, sendInput],
  );

  const onTouchEnd = useCallback(
    (event: ReactTouchEvent<HTMLDivElement>) => {
      const touches = event.nativeEvent.touches;
      const now = Date.now();

      // Single finger up — tap detection.
      if (touches.length === 0 && singleTouchStartRef.current) {
        clearLongPressTimer();
        const start = singleTouchStartRef.current;
        if (!singleTouchMovedRef.current) {
          // Short tap → left click.
          emitClick(start.x, start.y, 0); // button 0 = left
        }
        singleTouchStartRef.current = null;
        singleTouchMovedRef.current = false;
      }

      // Two finger up — tap detection for right-click.
      if (touches.length === 0 && twoTouchStartRef.current) {
        const { a, b } = twoTouchStartRef.current;
        if (!twoTouchMovedRef.current) {
          const duration = now - Math.max(a.t, b.t);
          if (duration < TWO_FINGER_TAP_MAX_DURATION) {
            // Two-finger tap → right-click.
            const mid = midpoint(a, b);
            emitClick(mid.x, mid.y, 2); // button 2 = right
          }
        }
        twoTouchStartRef.current = null;
        twoTouchMovedRef.current = false;
        lastPinchDistRef.current = 0;
      }
    },
    [clearLongPressTimer, emitClick],
  );

  return {
    onTouchStart,
    onTouchMove,
    onTouchEnd,
  };
}
