/**
 * useGestures hook — gesture recognition tests.
 */
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useGestures, type InputEventPayload } from './useGestures';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

// ── Harness ──

type HarnessProps = {
  viewportWidth: number;
  viewportHeight: number;
  sendInput: (event: InputEventPayload) => void;
  onHandlers?: (handlers: ReturnType<typeof useGestures>) => void;
};

function Harness({ viewportWidth, viewportHeight, sendInput, onHandlers }: HarnessProps) {
  const handlers = useGestures({ sendInput, viewportWidth, viewportHeight });
  if (onHandlers) onHandlers(handlers);
  return (
    <div
      data-testid="surface"
      onTouchStart={handlers.onTouchStart}
      onTouchMove={handlers.onTouchMove}
      onTouchEnd={handlers.onTouchEnd}
    />
  );
}

// ── Tests ──

describe('useGestures', () => {
  let container: HTMLDivElement;
  let root: Root;
  let sendInput: ReturnType<typeof vi.fn<(event: InputEventPayload) => void>>;
  let surfaceEl: HTMLDivElement;

  beforeEach(() => {
    sendInput = vi.fn<(event: InputEventPayload) => void>();
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);

    act(() => {
      root.render(
        <Harness
          viewportWidth={1280}
          viewportHeight={800}
          sendInput={sendInput}
        />,
      );
    });

    surfaceEl = container.querySelector('[data-testid="surface"]')!;
    // Mock getBoundingClientRect on the surface element.
    surfaceEl.getBoundingClientRect = () => ({
      x: 0, y: 0, width: 1000, height: 500,
      top: 0, left: 0, right: 1000, bottom: 500,
      toJSON: () => {},
    });
  });

  afterEach(() => {
    act(() => { root.unmount(); });
    container.remove();
    vi.restoreAllMocks();
  });

  function createTouch(identifier: number, x: number, y: number): Touch {
    return { identifier, clientX: x, clientY: y, target: surfaceEl, pageX: x, pageY: y, screenX: x, screenY: y, radiusX: 1, radiusY: 1, rotationAngle: 0, force: 1 } as unknown as Touch;
  }

  function dispatchTouchEvent(type: string, touches: Touch[], changedTouches?: Touch[]) {
    const event = new TouchEvent(type, {
      bubbles: true,
      cancelable: true,
      touches: type === 'touchend' ? [] : touches,
      targetTouches: type === 'touchend' ? [] : touches,
      changedTouches: changedTouches ?? touches,
    });
    surfaceEl.dispatchEvent(event);
  }

  it('single tap emits left click (mouse_down + mouse_up)', () => {
    const touches = [createTouch(0, 500, 250)];
    dispatchTouchEvent('touchstart', touches);
    dispatchTouchEvent('touchend', touches);

    expect(sendInput).toHaveBeenCalledTimes(2);
    expect(sendInput).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'mouse_down', button: 0 }),
    );
    expect(sendInput).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'mouse_up', button: 0 }),
    );
  });

  it('single tap coordinates are normalized to viewport', () => {
    // Touch at center of element: (500, 250) in 1000x500 element → (640, 400) in 1280x800 viewport.
    const touches = [createTouch(0, 500, 250)];
    dispatchTouchEvent('touchstart', touches);
    dispatchTouchEvent('touchend', touches);

    const firstCall = sendInput.mock.calls[0][0] as InputEventPayload;
    expect(firstCall.x).toBeCloseTo(640, 0);
    expect(firstCall.y).toBeCloseTo(400, 0);
  });

  it('long press emits right-click', () => {
    vi.useFakeTimers();

    const touches = [createTouch(0, 500, 250)];
    dispatchTouchEvent('touchstart', touches);

    // Advance past the long press threshold (500ms).
    act(() => { vi.advanceTimersByTime(600); });

    expect(sendInput).toHaveBeenCalledTimes(2);
    expect(sendInput).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'mouse_down', button: 2 }),
    );
    expect(sendInput).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'mouse_up', button: 2 }),
    );

    vi.useRealTimers();
  });

  it('long press cancelled if finger moves', () => {
    vi.useFakeTimers();

    const startTouches = [createTouch(0, 500, 250)];
    dispatchTouchEvent('touchstart', startTouches);

    // Move finger beyond tap threshold.
    const moveTouches = [createTouch(0, 520, 270)];
    dispatchTouchEvent('touchmove', moveTouches);

    act(() => { vi.advanceTimersByTime(600); });

    // Should not have emitted a right-click.
    expect(sendInput).not.toHaveBeenCalled();

    vi.useRealTimers();
  });

  it('two-finger tap emits right-click', () => {
    const startTouches = [createTouch(0, 400, 200), createTouch(1, 600, 300)];
    dispatchTouchEvent('touchstart', startTouches);

    // Release both quickly.
    dispatchTouchEvent('touchend', startTouches);

    expect(sendInput).toHaveBeenCalledTimes(2);
    expect(sendInput).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'mouse_down', button: 2 }),
    );
    expect(sendInput).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'mouse_up', button: 2 }),
    );
  });

  it('two-finger drag emits scroll events', () => {
    const startTouches = [createTouch(0, 400, 200), createTouch(1, 600, 300)];
    dispatchTouchEvent('touchstart', startTouches);

    // Move both fingers down.
    const moveTouches = [createTouch(0, 400, 250), createTouch(1, 600, 350)];
    dispatchTouchEvent('touchmove', moveTouches);

    expect(sendInput).toHaveBeenCalledWith(
      expect.objectContaining({ type: 'scroll' }),
    );
    const scrollEvt = sendInput.mock.calls[0][0] as InputEventPayload;
    // deltaY should be non-zero (vertical scroll).
    expect(scrollEvt.deltaY).not.toBe(0);
  });

  it('pinch in emits zoom scroll with ctrl modifier', () => {
    const startTouches = [createTouch(0, 450, 250), createTouch(1, 550, 250)];
    dispatchTouchEvent('touchstart', startTouches);

    // Pinch outward (zoom in) — increase distance between fingers.
    const moveTouches = [createTouch(0, 350, 250), createTouch(1, 650, 250)];
    dispatchTouchEvent('touchmove', moveTouches);

    // Find the zoom event (one with modifiers: 2).
    const zoomCalls = sendInput.mock.calls.filter(
      (call: unknown[]) => (call[0] as InputEventPayload).modifiers === 2,
    );
    expect(zoomCalls.length).toBeGreaterThan(0);
    const zoomEvt = zoomCalls[0][0] as InputEventPayload;
    expect(zoomEvt.type).toBe('scroll');
    expect(zoomEvt.deltaY).toBeLessThan(0); // zoom in = scroll up
  });

  it('two-finger tap does not emit right-click if fingers moved', () => {
    const startTouches = [createTouch(0, 400, 200), createTouch(1, 600, 300)];
    dispatchTouchEvent('touchstart', startTouches);

    // Move fingers significantly.
    const moveTouches = [createTouch(0, 400, 260), createTouch(1, 600, 360)];
    dispatchTouchEvent('touchmove', moveTouches);

    const endTouches = [createTouch(0, 400, 260), createTouch(1, 600, 360)];
    dispatchTouchEvent('touchend', endTouches);

    // Should NOT emit a click (it was a drag, not a tap).
    const clickCalls = sendInput.mock.calls.filter(
      (call: unknown[]) => (call[0] as InputEventPayload).type === 'mouse_down',
    );
    expect(clickCalls.length).toBe(0);
  });
});
