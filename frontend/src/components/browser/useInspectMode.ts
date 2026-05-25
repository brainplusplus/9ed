import { useCallback, useEffect, useRef, useState } from 'react';
import type { BoxModelLayers, BrowserElementSelection, BrowserTab, ParentChainItem } from '../../types';
import { useChatStore } from '../../stores/chat';

/* ── Types ── */

export type InspectRect = {
  top: number;
  left: number;
  width: number;
  height: number;
};

export type InspectTooltipData = {
  top: number;
  left: number;
  label: string;
  sublabel?: string;
};

export type InspectState = {
  hoveredBoxModel: BoxModelLayers | null;
  hoveredRect: InspectRect | null;
  tooltip: InspectTooltipData | null;
  selectedElement: Element | null;
};

export type UseInspectModeResult = {
  inspectState: InspectState;
  inspectMode: boolean;
  toggleInspectMode: () => void;
  clearSelection: () => void;
  /** Ref to the overlay canvas element */
  overlayCanvasRef: React.RefObject<HTMLCanvasElement | null>;
};

/* ── Chrome DevTools color scheme ── */

export const COLORS = {
  content:  'rgba(111, 168, 220, 0.28)',
  padding:  'rgba(147, 196, 125, 0.22)',
  border:   'rgba(255, 210, 92, 0.22)',
  margin:   'rgba(246, 178, 107, 0.28)',
  contentBorder:  'rgba(111, 168, 220, 0.7)',
  paddingBorder:  'rgba(147, 196, 125, 0.6)',
  borderBorder:   'rgba(255, 210, 92, 0.6)',
  marginBorder:   'rgba(246, 178, 107, 0.7)',
  ruler:     'rgba(255, 255, 255, 0.25)',
  dimension: 'rgba(255, 255, 255, 0.8)',
  label:     'rgba(15, 23, 42, 0.92)',
  selected:  'rgba(59, 130, 246, 0.35)',
  selectedBorder: 'rgba(59, 130, 246, 0.9)',
} as const;

/* ── Smart element hit-test ── */

function resolveTargetElement(raw: EventTarget | Element | null, doc: Document, win: Window): Element | null {
  // Cross-frame instanceof check: use nodeType instead of instanceof Element,
  // because iframe elements have a different Element constructor than the parent window.
  if (!raw || (raw as Node).nodeType !== 1) return null;

  let el: Element | null = raw as Element;

  // Walk up: skip invisible, zero-size, display:none, pointer-events:none
  while (el) {
    const cs = win.getComputedStyle(el);
    if (
      cs.display === 'none' ||
      cs.visibility === 'hidden' ||
      parseFloat(cs.opacity) === 0
    ) {
      el = el.parentElement;
      continue;
    }
    // Skip elements with pointer-events: none (unless they're the body/html)
    if (
      cs.pointerEvents === 'none' &&
      el !== doc.body &&
      el !== doc.documentElement
    ) {
      el = el.parentElement;
      continue;
    }
    const rect = el.getBoundingClientRect();
    if (rect.width > 0 && rect.height > 0) break;
    el = el.parentElement;
  }

  return el;
}

/* ── Box model extraction ── */

function extractBoxModel(element: Element, win: Window): BoxModelLayers {
  const cs = win.getComputedStyle(element);
  const rect = element.getBoundingClientRect();

  const parse = (v: string) => parseFloat(v) || 0;

  const mt = parse(cs.marginTop), mr = parse(cs.marginRight);
  const mb = parse(cs.marginBottom), ml = parse(cs.marginLeft);
  const bt = parse(cs.borderTopWidth), br = parse(cs.borderRightWidth);
  const bb = parse(cs.borderBottomWidth), bl = parse(cs.borderLeftWidth);
  const pt = parse(cs.paddingTop), pr = parse(cs.paddingRight);
  const pb = parse(cs.paddingBottom), pl = parse(cs.paddingLeft);

  const contentWidth = Math.max(0, rect.width - bl - br - pl - pr);
  const contentHeight = Math.max(0, rect.height - bt - bb - pt - pb);

  return {
    margin: { top: mt, right: mr, bottom: mb, left: ml },
    border: { top: bt, right: br, bottom: bb, left: bl },
    padding: { top: pt, right: pr, bottom: pb, left: pl },
    contentRect: {
      x: rect.x + bl + pl,
      y: rect.y + bt + pt,
      width: contentWidth,
      height: contentHeight,
    },
  };
}

/* ── Element label builders ── */

function buildElementLabel(element: Element): string {
  let label = element.tagName.toLowerCase();
  if (element.id) label += `#${element.id}`;
  const cls = (element.getAttribute('class') ?? '')
    .trim().split(/\s+/).filter(Boolean).slice(0, 3).join('.');
  if (cls) label += `.${cls}`;
  return label;
}

function buildDimensionLabel(rect: DOMRect, cs: CSSStyleDeclaration): string {
  return `${Math.round(rect.width)}×${Math.round(rect.height)}  ${cs.display}`;
}

function buildBoxModelLabel(box: BoxModelLayers): string {
  const parts: string[] = [];
  const m = box.margin;
  if (m.top || m.right || m.bottom || m.left) {
    parts.push(`margin:${Math.round(m.top)} ${Math.round(m.right)} ${Math.round(m.bottom)} ${Math.round(m.left)}`);
  }
  const p = box.padding;
  if (p.top || p.right || p.bottom || p.left) {
    parts.push(`padding:${Math.round(p.top)} ${Math.round(p.right)} ${Math.round(p.bottom)} ${Math.round(p.left)}`);
  }
  return parts.join('  ');
}

/* ── Unique CSS selector ── */

function buildUniqueSelector(element: Element, doc: Document): string {
  const parts: string[] = [];
  let current: Element | null = element;
  let depth = 0;

  while (current && current !== doc.documentElement && depth < 8) {
    let part = current.tagName.toLowerCase();

    if (current.id) {
      // ID is unique — stop here
      part += `#${CSS.escape(current.id)}`;
      parts.unshift(part);
      break;
    }

    // Build nth-child selector
    const parentElement: Element | null = current.parentElement;
    if (parentElement) {
      const siblings = Array.from(parentElement.children);
      const sameTagSiblings = siblings.filter(
        (s: Element) => s.tagName === current!.tagName,
      );
      if (sameTagSiblings.length > 1) {
        const idx = sameTagSiblings.indexOf(current!) + 1;
        part += `:nth-child(${idx})`;
      }
    }

    const cls = (current.getAttribute('class') ?? '').trim().split(/\s+/).filter(Boolean).slice(0, 2);
    if (cls.length > 0) {
      part += cls.map((c) => `.${CSS.escape(c)}`).join('');
    }

    parts.unshift(part);
    current = parentElement;
    depth++;
  }

  if (current === doc.documentElement) {
    parts.unshift('html');
  }

  return parts.join(' > ');
}

/* ── Parent chain ── */

function buildParentChain(element: Element, maxDepth = 6): ParentChainItem[] {
  const chain: ParentChainItem[] = [];
  let current: Element | null = element;
  let depth = 0;

  while (current && current !== current.ownerDocument.documentElement && depth < maxDepth) {
    chain.unshift({
      tagName: current.tagName.toLowerCase(),
      id: current.id || undefined,
      classes: (current.getAttribute('class') ?? '').trim().split(/\s+/).filter(Boolean),
    });
    current = current.parentElement;
    depth++;
  }

  return chain;
}

/* ── Event listener extraction (best effort) ── */

function extractEventListeners(element: Element): Array<{ type: string; handlerBody: string }> {
  const listeners: Array<{ type: string; handlerBody: string }> = [];

  // Standard event types to check
  const eventTypes = [
    'click', 'dblclick', 'mousedown', 'mouseup', 'mouseover', 'mousemove', 'mouseout',
    'keydown', 'keyup', 'keypress', 'focus', 'blur', 'change', 'input', 'submit',
    'scroll', 'resize', 'load', 'error', 'touchstart', 'touchend', 'touchmove',
  ];

  // Check inline event handlers (on* attributes)
  for (const type of eventTypes) {
    const attr = element.getAttribute(`on${type}`);
    if (attr) {
      listeners.push({ type, handlerBody: attr.slice(0, 200) });
    }
  }

  // Note: getEventListeners() is only available in Chrome DevTools console
  // We cannot reliably detect addEventListener handlers from content script

  return listeners;
}

/* ── Accessibility info ── */

function extractAccessibilityInfo(
  element: Element,
  win: Window,
): BrowserElementSelection['accessibilityInfo'] {
  const cs = win.getComputedStyle(element);
  const role = element.getAttribute('role');
  const ariaLabel = element.getAttribute('aria-label') ?? element.getAttribute('aria-labelledby') ?? undefined;
  const tabIndexAttr = element.getAttribute('tabindex');
  const tabIndex = tabIndexAttr !== null ? parseInt(tabIndexAttr, 10) : undefined;

  // Determine focusable
  const focusableTags = new Set(['a', 'button', 'input', 'select', 'textarea', 'details', 'summary']);
  const isNativeFocusable = focusableTags.has(element.tagName.toLowerCase());
  const isTabbable = tabIndexAttr !== null && (tabIndex ?? -1) >= 0;
  const focusable = isNativeFocusable || isTabbable;

  // Contrast ratio estimation (simplified)
  let contrastRatio: string | undefined;
  const fg = cs.color;
  const bg = cs.backgroundColor;
  if (fg && bg && bg !== 'rgba(0, 0, 0, 0)' && bg !== 'transparent') {
    contrastRatio = `${fg} on ${bg}`;
  }

  return {
    role: role || undefined,
    label: ariaLabel,
    tabIndex,
    focusable,
    contrastRatio,
  };
}

/* ── Full selection creation ── */

function createFullSelection(
  tab: BrowserTab,
  element: Element,
  frameWindow: Window,
): BrowserElementSelection {
  const doc = element.ownerDocument;
  const text = element.textContent?.trim().replace(/\s+/g, ' ') ?? '';
  const cs = frameWindow.getComputedStyle(element);
  const rect = element.getBoundingClientRect();
  const box = extractBoxModel(element, frameWindow);

  // Extract meaningful attributes
  const attrs: Record<string, string> = {};
  const meaningfulAttrs = new Set([
    'id', 'role', 'type', 'name', 'value', 'href', 'src', 'alt', 'title',
    'placeholder', 'disabled', 'readonly', 'required', 'checked', 'selected',
    'aria-label', 'aria-labelledby', 'aria-describedby', 'aria-expanded',
    'aria-selected', 'aria-checked', 'aria-hidden', 'aria-disabled',
    'data-testid', 'data-id', 'data-testid', 'for', 'action', 'method',
    'target', 'rel', 'download', 'pattern', 'min', 'max', 'step', 'maxlength',
  ]);
  for (const attr of Array.from(element.attributes)) {
    if (
      meaningfulAttrs.has(attr.name) ||
      (attr.name.startsWith('data-') && attr.name !== 'data-nine-proxy-runtime')
    ) {
      attrs[attr.name] = attr.value;
    }
  }

  return {
    url: tab.url,
    title: tab.title,
    tagName: element.tagName,
    role: element.getAttribute('role') ?? undefined,
    text: text.slice(0, 600) || undefined,
    selector: buildSelectorPath(element),
    uniqueSelector: buildUniqueSelector(element, doc),
    outerHTML: element.outerHTML.slice(0, 4000),
    attributes: Object.keys(attrs).length > 0 ? attrs : undefined,
    computedStyle: {
      display: cs.display,
      position: cs.position,
      width: cs.width,
      height: cs.height,
      color: cs.color,
      backgroundColor: cs.backgroundColor,
      fontSize: cs.fontSize,
      fontFamily: cs.fontFamily,
      fontWeight: cs.fontWeight,
      lineHeight: cs.lineHeight,
      textAlign: cs.textAlign,
      margin: cs.margin,
      padding: cs.padding,
      border: cs.border,
      borderRadius: cs.borderRadius,
      overflow: cs.overflow,
      opacity: cs.opacity,
      visibility: cs.visibility,
      zIndex: cs.zIndex,
      ...(cs.display.includes('flex') ? { flex: cs.flex } : {}),
      ...(cs.display.includes('grid') ? { grid: cs.gridTemplateColumns } : {}),
      ...(cs.gap !== 'normal' ? { gap: cs.gap } : {}),
      ...(cs.position !== 'static' ? {
        top: cs.top,
        left: cs.left,
        right: cs.right,
        bottom: cs.bottom,
      } : {}),
    },
    boundingRect: {
      width: Math.round(rect.width),
      height: Math.round(rect.height),
      x: Math.round(rect.x),
      y: Math.round(rect.y),
    },
    boxModel: box,
    parentChain: buildParentChain(element),
    accessibilityInfo: extractAccessibilityInfo(element, frameWindow),
    eventListeners: extractEventListeners(element),
  };
}

function buildSelectorPath(element: Element): string {
  const parts: string[] = [];
  let current: Element | null = element;
  while (current && parts.length < 5) {
    let part = current.tagName.toLowerCase();
    if (current.id) {
      part += `#${current.id}`;
      parts.unshift(part);
      break;
    }
    const cls = (current.getAttribute('class') ?? '').trim().split(/\s+/).filter(Boolean).slice(0, 2).join('.');
    if (cls) part += `.${cls}`;
    parts.unshift(part);
    current = current.parentElement;
  }
  return parts.join(' > ');
}

/* ── DOM navigation helpers ── */

function navigateUp(element: Element): Element | null {
  return element.parentElement;
}

function navigateDown(element: Element): Element | null {
  // Prefer first visible child
  const win = element.ownerDocument.defaultView;
  for (const child of Array.from(element.children)) {
    if (!win) return child;
    const cs = win.getComputedStyle(child);
    if (cs.display !== 'none' && cs.visibility !== 'hidden') {
      const rect = child.getBoundingClientRect();
      if (rect.width > 0 && rect.height > 0) return child;
    }
  }
  return element.firstElementChild;
}

function navigateNext(element: Element): Element | null {
  const win = element.ownerDocument.defaultView;
  let sibling = element.nextElementSibling;
  while (sibling && win) {
    const cs = win.getComputedStyle(sibling);
    if (cs.display !== 'none' && cs.visibility !== 'hidden') {
      const rect = sibling.getBoundingClientRect();
      if (rect.width > 0 && rect.height > 0) return sibling;
    }
    sibling = sibling.nextElementSibling;
  }
  return sibling;
}

function navigatePrev(element: Element): Element | null {
  const win = element.ownerDocument.defaultView;
  let sibling = element.previousElementSibling;
  while (sibling && win) {
    const cs = win.getComputedStyle(sibling);
    if (cs.display !== 'none' && cs.visibility !== 'hidden') {
      const rect = sibling.getBoundingClientRect();
      if (rect.width > 0 && rect.height > 0) return sibling;
    }
    sibling = sibling.previousElementSibling;
  }
  return sibling;
}

/* ── Animation interpolation ── */

function lerpBoxModel(
  prev: BoxModelLayers | null,
  target: BoxModelLayers,
  t: number,
): BoxModelLayers {
  if (!prev) return target;

  const lerpEdges = (
    p: { top: number; right: number; bottom: number; left: number },
    q: { top: number; right: number; bottom: number; left: number },
  ) => ({
    top: p.top + (q.top - p.top) * t,
    right: p.right + (q.right - p.right) * t,
    bottom: p.bottom + (q.bottom - p.bottom) * t,
    left: p.left + (q.left - p.left) * t,
  });

  const lerpRect = (
    p: { x: number; y: number; width: number; height: number },
    q: { x: number; y: number; width: number; height: number },
  ) => ({
    x: p.x + (q.x - p.x) * t,
    y: p.y + (q.y - p.y) * t,
    width: p.width + (q.width - p.width) * t,
    height: p.height + (q.height - p.height) * t,
  });

  return {
    margin: lerpEdges(prev.margin, target.margin),
    border: lerpEdges(prev.border, target.border),
    padding: lerpEdges(prev.padding, target.padding),
    contentRect: lerpRect(prev.contentRect, target.contentRect),
  };
}

/* ── Main hook ── */

export function useInspectMode(
  iframeRef: React.RefObject<HTMLIFrameElement | null>,
  stageRef: React.RefObject<HTMLDivElement | null>,
  activeTab: BrowserTab | null,
): UseInspectModeResult {
  const [inspectMode, setInspectMode] = useState(false);
  const [hoveredBoxModel, setHoveredBoxModel] = useState<BoxModelLayers | null>(null);
  const [hoveredRect, setHoveredRect] = useState<InspectRect | null>(null);
  const [tooltip, setTooltip] = useState<InspectTooltipData | null>(null);
  const [selectedElement, setSelectedElement] = useState<Element | null>(null);
  const overlayCanvasRef = useRef<HTMLCanvasElement | null>(null);

  const cleanupRef = useRef<(() => void) | null>(null);
  const animFrameRef = useRef<number>(0);
  const prevBoxModelRef = useRef<BoxModelLayers | null>(null);
  const targetBoxModelRef = useRef<BoxModelLayers | null>(null);
  const keyboardElementRef = useRef<Element | null>(null);

  const setBrowserSelection = useChatStore((s) => s.setBrowserSelection);
  const toggleUseActiveBrowser = useChatStore((s) => s.toggleUseActiveBrowser);
  const useActiveBrowser = useChatStore((s) => s.useActiveBrowser);

  const clearSelection = useCallback(() => {
    setBrowserSelection(null);
    setSelectedElement(null);
    keyboardElementRef.current = null;
  }, [setBrowserSelection]);

  const toggleInspectMode = useCallback(() => {
    setInspectMode((v) => !v);
  }, []);

  /* ── Animation loop ── */

  useEffect(() => {
    if (!inspectMode) {
      prevBoxModelRef.current = null;
      targetBoxModelRef.current = null;
      return;
    }

    const LERP_FACTOR = 0.2;
    let running = true;
    let frameCount = 0;

    const tick = () => {
      if (!running) return;

      const target = targetBoxModelRef.current;
      if (target) {
        const interpolated = lerpBoxModel(prevBoxModelRef.current, target, LERP_FACTOR);
        prevBoxModelRef.current = interpolated;
        setHoveredBoxModel(interpolated);
        frameCount++;
      } else if (prevBoxModelRef.current) {
        // No target (mouseleave) — clear residual highlight
        prevBoxModelRef.current = null;
        setHoveredBoxModel(null);
      }

      animFrameRef.current = requestAnimationFrame(tick);
    };

    animFrameRef.current = requestAnimationFrame(tick);

    return () => {
      running = false;
      cancelAnimationFrame(animFrameRef.current);
    };
  }, [inspectMode]);

  /* ── Main inspect binding ── */

  useEffect(() => {
    if (!inspectMode) {
      cleanupRef.current?.();
      cleanupRef.current = null;
      setHoveredBoxModel(null);
      setHoveredRect(null);
      setTooltip(null);
      return;
    }

    const iframe = iframeRef.current;
    const stage = stageRef.current;
    if (!iframe || !stage) return;

    const shell = iframe.parentElement;
    if (!shell) return;

    const bindInspect = () => {
      try {
        const frameWindow = iframe.contentWindow;
        const doc = iframe.contentDocument;
        if (!frameWindow || !doc) return;

        /* ── Coordinate math ──
         * Mouse events fire on the SHELL (parent of iframe + canvas).
         * We convert shell-relative mouse coords → iframe content coords
         * using the visual scale factor, then use elementFromPoint()
         * inside the iframe to find the element under cursor. */

        const getScale = () => {
          const iframeVisual = iframe.getBoundingClientRect();
          return {
            scaleX: iframeVisual.width / Math.max(1, iframe.clientWidth),
            scaleY: iframeVisual.height / Math.max(1, iframe.clientHeight),
          };
        };

        const shellToIframeContent = (clientX: number, clientY: number) => {
          const iframeRect = iframe.getBoundingClientRect();
          const { scaleX, scaleY } = getScale();
          const relX = clientX - iframeRect.left;
          const relY = clientY - iframeRect.top;
          // Convert from visual coords to content coords
          return {
            x: relX / scaleX + frameWindow.scrollX,
            y: relY / scaleY + frameWindow.scrollY,
            inBounds: relX >= 0 && relY >= 0 && relX <= iframeRect.width && relY <= iframeRect.height,
          };
        };

        /** Maps an iframe content rect to shell-relative coordinates for canvas/tooltip. */
        const toShellCoords = (
          elementRect: { x: number; y: number; width: number; height: number },
        ) => {
          const { scaleX, scaleY } = getScale();
          return {
            left: elementRect.x * scaleX,
            top: elementRect.y * scaleY,
            width: Math.max(1, elementRect.width * scaleX),
            height: Math.max(1, elementRect.height * scaleY),
          };
        };

        const updateHighlight = (element: Element, clientX?: number, clientY?: number) => {
          const cs = frameWindow.getComputedStyle(element);
          const rect = element.getBoundingClientRect();
          const box = extractBoxModel(element, frameWindow);

          // Set target for animation
          targetBoxModelRef.current = box;

          // Set outer rect (for ruler lines positioning) — shell-relative
          const outerCoords = toShellCoords({
            x: rect.x - box.margin.left,
            y: rect.y - box.margin.top,
            width: rect.width + box.margin.left + box.margin.right,
            height: rect.height + box.margin.top + box.margin.bottom,
          });
          setHoveredRect(outerCoords);

          // Tooltip — positioned relative to the shell
          const shellRect = shell.getBoundingClientRect();
          const label = buildElementLabel(element);
          const sublabel = `${buildDimensionLabel(rect, cs)}  |  ${buildBoxModelLabel(box)}`;
          const shellW = shellRect.width;
          let tooltipX: number;
          let tooltipY = outerCoords.top - 32;

          if (tooltipY < 2) {
            tooltipY = outerCoords.top + outerCoords.height + 4;
          }
          if (clientX !== undefined && clientY !== undefined) {
            tooltipX = clientX - shellRect.left + 14;
          } else {
            tooltipX = outerCoords.left;
          }
          const maxLeft = Math.max(0, shellW - 320);
          tooltipX = Math.max(4, Math.min(tooltipX, maxLeft));

          setTooltip({ top: tooltipY, left: tooltipX, label, sublabel });
        };

        /* ── Hit-test using elementFromPoint ── */
        const hitTest = (clientX: number, clientY: number): Element | null => {
          const coords = shellToIframeContent(clientX, clientY);
          if (!coords.inBounds) return null;
          const el = doc.elementFromPoint(coords.x, coords.y);
          if (!el) return null;
          // Walk up: skip invisible, zero-size, display:none
          return resolveTargetElement(el, doc, frameWindow);
        };

        /* ── Event handlers — bound to SHELL, not iframe doc ── */

        const handleMove = (event: MouseEvent) => {
          const el = hitTest(event.clientX, event.clientY);
          if (!el) {
            targetBoxModelRef.current = null;
            setHoveredRect(null);
            setTooltip(null);
            return;
          }
          keyboardElementRef.current = el;
          updateHighlight(el, event.clientX, event.clientY);
        };

        const handleLeave = () => {
          targetBoxModelRef.current = null;
          setHoveredRect(null);
          setTooltip(null);
        };

        const handleClick = (event: MouseEvent) => {
          const el = hitTest(event.clientX, event.clientY);
          if (!el || !activeTab) {
            return;
          }

          event.preventDefault();
          event.stopPropagation();

          const selection = createFullSelection(activeTab, el, frameWindow);
          setBrowserSelection(selection);
          setSelectedElement(el);
          keyboardElementRef.current = el;

          if (!useActiveBrowser) {
            toggleUseActiveBrowser();
          }

          // Keep the highlight on selected element
          updateHighlight(el);
          setInspectMode(false);
        };

        /* Keyboard navigation */
        const handleKeyDown = (event: KeyboardEvent) => {
          if (event.key === 'Escape') {
            setInspectMode(false);
            return;
          }

          const navTarget = keyboardElementRef.current;
          if (!navTarget) return;

          let next: Element | null = null;

          switch (event.key) {
            case 'ArrowUp':
              next = navigateUp(navTarget);
              break;
            case 'ArrowDown':
              next = navigateDown(navTarget);
              break;
            case 'ArrowRight':
              next = navigateNext(navTarget);
              break;
            case 'ArrowLeft':
              next = navigatePrev(navTarget);
              break;
            case 'Enter':
              if (activeTab) {
                const selection = createFullSelection(activeTab, navTarget, frameWindow);
                setBrowserSelection(selection);
                setSelectedElement(navTarget);
                if (!useActiveBrowser) toggleUseActiveBrowser();
                updateHighlight(navTarget);
                setInspectMode(false);
              }
              return;
            default:
              return;
          }

          if (next && next !== doc.documentElement) {
            event.preventDefault();
            keyboardElementRef.current = next;
            updateHighlight(next);
          }
        };

        /* Scroll-aware re-highlight */
        const handleScroll = () => {
          const el = keyboardElementRef.current;
          if (el && el.isConnected) {
            updateHighlight(el);
          } else {
            targetBoxModelRef.current = null;
            setHoveredRect(null);
            setTooltip(null);
          }
        };

        /* ── Bind events on SHELL (not iframe document) ── */
        shell.style.cursor = 'crosshair';
        shell.addEventListener('mousemove', handleMove, true);
        shell.addEventListener('mouseleave', handleLeave, true);
        shell.addEventListener('click', handleClick, true);

        // Scroll still needs to be on iframe window
        frameWindow.addEventListener('scroll', handleScroll, true);
        window.addEventListener('keydown', handleKeyDown);

        cleanupRef.current = () => {
          shell.style.cursor = '';
          shell.removeEventListener('mousemove', handleMove, true);
          shell.removeEventListener('mouseleave', handleLeave, true);
          shell.removeEventListener('click', handleClick, true);
          frameWindow.removeEventListener('scroll', handleScroll, true);
          window.removeEventListener('keydown', handleKeyDown);
        };
      } catch {
        setInspectMode(false);
      }
    };

    if (iframe.contentDocument?.readyState === 'complete') {
      bindInspect();
    } else {
      const handleLoad = () => {
        bindInspect();
      };
      iframe.addEventListener('load', handleLoad, { once: true });
      cleanupRef.current = () => iframe.removeEventListener('load', handleLoad);
    }

    return () => {
      cleanupRef.current?.();
      cleanupRef.current = null;
    };
  }, [
    activeTab,
    inspectMode,
    iframeRef,
    stageRef,
    setBrowserSelection,
    toggleUseActiveBrowser,
    useActiveBrowser,
  ]);

  return {
    inspectState: { hoveredBoxModel, hoveredRect, tooltip, selectedElement },
    inspectMode,
    toggleInspectMode,
    clearSelection,
    overlayCanvasRef,
  };
}
