import { useEffect, useRef, useState, useCallback } from 'react';
import type { BoxModelLayers, BrowserElementSelection } from '../../types';
import type { InspectRect, InspectTooltipData } from './useInspectMode';
import { COLORS } from './useInspectMode';

/* ── Types ── */

export type InspectOverlayProps = {
  boxModel: BoxModelLayers | null;
  outerRect: InspectRect | null;
  tooltip: InspectTooltipData | null;
  iframeRef: React.RefObject<HTMLIFrameElement | null>;
  inspectMode: boolean;
  selection: BrowserElementSelection | null;
};

export type MiniPanelProps = {
  selection: BrowserElementSelection | null;
  visible: boolean;
  onClose: () => void;
};

type RemoteInspectOverlayProps = {
  hoverSelection: BrowserElementSelection | null;
  selection: BrowserElementSelection | null;
  tooltip: InspectTooltipData | null;
  inspectMode: boolean;
  scaleX: number;
  scaleY: number;
};

/* ── Canvas overlay drawing ── */

export function InspectOverlay({
  boxModel,
  outerRect,
  tooltip,
  iframeRef,
  inspectMode,
}: InspectOverlayProps) {
  const canvasRef = useRef<HTMLCanvasElement>(null);

  // Sync canvas size with iframe visual size
  useEffect(() => {
    const canvas = canvasRef.current;
    const iframe = iframeRef.current;
    const shell = canvas?.parentElement;
    if (!canvas || !iframe || !shell) return;

    const observer = new ResizeObserver(() => {
      const rect = shell.getBoundingClientRect();
      const dpr = window.devicePixelRatio || 1;
      canvas.width = Math.round(rect.width * dpr);
      canvas.height = Math.round(rect.height * dpr);
      canvas.style.width = `${rect.width}px`;
      canvas.style.height = `${rect.height}px`;
    });

    observer.observe(shell);
    return () => observer.disconnect();
  }, [iframeRef]);

  // Draw box model layers
  const draw = useCallback(() => {
    const canvas = canvasRef.current;
    const iframe = iframeRef.current;
    if (!canvas || !iframe || !boxModel) return;

    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    const dpr = window.devicePixelRatio || 1;
    console.log('[overlay] draw()', {
      canvasW: canvas.clientWidth, canvasH: canvas.clientHeight,
      iframeW: iframe.clientWidth, iframeH: iframe.clientHeight,
      boxContentRect: boxModel.contentRect,
    });
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.save();
    ctx.scale(dpr, dpr);

    // Scale: visual iframe size / content size
    const scaleX = canvas.clientWidth / Math.max(1, iframe.clientWidth);
    const scaleY = canvas.clientHeight / Math.max(1, iframe.clientHeight);

    // Transform iframe content coords to canvas coords (no offset needed)
    const tx = (x: number, y: number) => ({
      x: x * scaleX,
      y: y * scaleY,
    });

    // Scale a dimension
    const sd = (v: number, scale: number) => v * scale;

    const content = boxModel.contentRect;
    const pad = boxModel.padding;
    const brd = boxModel.border;
    const mgn = boxModel.margin;

    // Compute 4 nested rects (from outermost to innermost)
    const marginRect = {
      x: content.x - pad.left - brd.left - mgn.left,
      y: content.y - pad.top - brd.top - mgn.top,
      w: content.width + pad.left + pad.right + brd.left + brd.right + mgn.left + mgn.right,
      h: content.height + pad.top + pad.bottom + brd.top + brd.bottom + mgn.top + mgn.bottom,
    };
    const borderRect = {
      x: content.x - pad.left - brd.left,
      y: content.y - pad.top - brd.top,
      w: content.width + pad.left + pad.right + brd.left + brd.right,
      h: content.height + pad.top + pad.bottom + brd.top + brd.bottom,
    };
    const paddingRect = {
      x: content.x - pad.left,
      y: content.y - pad.top,
      w: content.width + pad.left + pad.right,
      h: content.height + pad.top + pad.bottom,
    };

    // Layer 1: Margin (orange)
    const mtl = tx(marginRect.x, marginRect.y);
    const mtlw = sd(marginRect.w, scaleX);
    const mtlh = sd(marginRect.h, scaleY);
    drawFilledRect(ctx, mtl.x, mtl.y, mtlw, mtlh, COLORS.margin, COLORS.marginBorder);

    // Layer 2: Border (yellow) — paints over margin inner area
    const btl = tx(borderRect.x, borderRect.y);
    drawFilledRect(ctx, btl.x, btl.y, sd(borderRect.w, scaleX), sd(borderRect.h, scaleY), COLORS.border, COLORS.borderBorder);

    // Layer 3: Padding (green)
    const ptl = tx(paddingRect.x, paddingRect.y);
    drawFilledRect(ctx, ptl.x, ptl.y, sd(paddingRect.w, scaleX), sd(paddingRect.h, scaleY), COLORS.padding, COLORS.paddingBorder);

    // Layer 4: Content (blue)
    const ctl = tx(content.x, content.y);
    drawFilledRect(ctx, ctl.x, ctl.y, sd(content.width, scaleX), sd(content.height, scaleY), COLORS.content, COLORS.contentBorder);

    // Layer 5: Ruler lines (from outer rect to canvas edges)
    if (outerRect) {
      drawRulers(ctx, outerRect, canvas.width / dpr, canvas.height / dpr);
    }

    // Layer 6: Dimension labels
    drawDimensionLabels(ctx, boxModel, tx, scaleX, scaleY);

    ctx.restore();
  }, [boxModel, outerRect, iframeRef]);

  // Redraw on every change
  // Redraw on every change
  useEffect(() => {
    if (!boxModel) {
      // Clear canvas when no hover
      const canvas = canvasRef.current;
      if (canvas) {
        const ctx = canvas.getContext('2d');
        if (ctx) ctx.clearRect(0, 0, canvas.width, canvas.height);
      }
      return;
    }
    draw();
  }, [boxModel, draw]);

  return (
    <>
      <canvas
        ref={canvasRef}
        className="browser-inspect-canvas"
        style={{ pointerEvents: 'none' }}
      />
      {tooltip && inspectMode && (
        <div className="browser-inspect-tooltip-v2" style={{ top: tooltip.top, left: tooltip.left }}>
          <span className="inspect-tooltip-label">{tooltip.label}</span>
          {tooltip.sublabel && (
            <span className="inspect-tooltip-sub">{tooltip.sublabel}</span>
          )}
        </div>
      )}
    </>
  );
}

export function RemoteInspectOverlay({
  hoverSelection,
  selection,
  tooltip,
  inspectMode,
  scaleX,
  scaleY,
}: RemoteInspectOverlayProps) {
  const hoverRects = hoverSelection?.boxModel ? scaleBoxRects(computeBoxRects(hoverSelection.boxModel), scaleX, scaleY) : null;
  const hoverRect = hoverSelection?.boundingRect ? computeBoundingRect(hoverSelection.boundingRect, scaleX, scaleY) : null;
  const selectedRect = selection?.boundingRect ? computeBoundingRect(selection.boundingRect, scaleX, scaleY) : null;
  const selectedTag = selection?.tagName?.toLowerCase();
  const hoverLabels = hoverSelection?.boxModel ? computeRemoteLabels(hoverSelection.boxModel, hoverRects) : null;

  return (
    <>
      {hoverRect && inspectMode && (
        <div className="browser-remote-overlay" aria-hidden="true">
          {hoverLabels && (
            <>
              <RemoteRulers rect={hoverRect} />
              <RemoteLabel text={hoverLabels.marginTop} left={hoverRect.left + hoverRect.width / 2} top={hoverRect.top - Math.max(12, (hoverSelection!.boxModel!.margin.top / 2) * scaleY)} />
              <RemoteLabel text={hoverLabels.marginBottom} left={hoverRect.left + hoverRect.width / 2} top={hoverRect.top + hoverRect.height + Math.max(12, (hoverSelection!.boxModel!.margin.bottom / 2) * scaleY)} />
              <RemoteLabel text={hoverLabels.marginLeft} left={hoverRect.left - Math.max(18, (hoverSelection!.boxModel!.margin.left / 2) * scaleX)} top={hoverRect.top + hoverRect.height / 2} />
              <RemoteLabel text={hoverLabels.marginRight} left={hoverRect.left + hoverRect.width + Math.max(18, (hoverSelection!.boxModel!.margin.right / 2) * scaleX)} top={hoverRect.top + hoverRect.height / 2} />
              <RemoteLabel text={hoverLabels.content} left={hoverRect.left + hoverRect.width / 2} top={hoverRect.top + hoverRect.height + 16} />
            </>
          )}
          <div
            className="browser-remote-hover-box"
            style={{
              left: `${hoverRect.left}px`,
              top: `${hoverRect.top}px`,
              width: `${hoverRect.width}px`,
              height: `${hoverRect.height}px`,
            }}
          />
        </div>
      )}
      {selectedRect && !inspectMode && (
        <div className="browser-remote-selected" aria-hidden="true">
          <div
            className="browser-remote-selected-box"
            style={{
              left: `${selectedRect.left}px`,
              top: `${selectedRect.top}px`,
              width: `${selectedRect.width}px`,
              height: `${selectedRect.height}px`,
            }}
          >
            {selectedTag && <span className="browser-remote-selected-badge">Selected {selectedTag}</span>}
          </div>
        </div>
      )}
      {tooltip && inspectMode && (
        <div className="browser-inspect-tooltip-v2" style={{ top: tooltip.top, left: tooltip.left }}>
          <span className="inspect-tooltip-label">{tooltip.label}</span>
          {tooltip.sublabel && (
            <span className="inspect-tooltip-sub">{tooltip.sublabel}</span>
          )}
        </div>
      )}
    </>
  );
}

function computeBoundingRect(rect: { width: number; height: number; x: number; y: number }, scaleX: number, scaleY: number) {
  return {
    left: rect.x * scaleX,
    top: rect.y * scaleY,
    width: rect.width * scaleX,
    height: rect.height * scaleY,
  };
}

/* ── Drawing helpers ── */

function drawFilledRect(
  ctx: CanvasRenderingContext2D,
  x: number,
  y: number,
  w: number,
  h: number,
  fill: string,
  stroke: string,
) {
  ctx.fillStyle = fill;
  ctx.fillRect(x, y, w, h);
  ctx.strokeStyle = stroke;
  ctx.lineWidth = 1;
  ctx.strokeRect(x + 0.5, y + 0.5, w - 1, h - 1);
}

function computeBoxRects(box: BoxModelLayers) {
  const content = {
    left: box.contentRect.x,
    top: box.contentRect.y,
    width: box.contentRect.width,
    height: box.contentRect.height,
  };
  const padding = {
    left: content.left - box.padding.left,
    top: content.top - box.padding.top,
    width: content.width + box.padding.left + box.padding.right,
    height: content.height + box.padding.top + box.padding.bottom,
  };
  const border = {
    left: padding.left - box.border.left,
    top: padding.top - box.border.top,
    width: padding.width + box.border.left + box.border.right,
    height: padding.height + box.border.top + box.border.bottom,
  };
  const margin = {
    left: border.left - box.margin.left,
    top: border.top - box.margin.top,
    width: border.width + box.margin.left + box.margin.right,
    height: border.height + box.margin.top + box.margin.bottom,
  };
  return { margin, border, padding, content };
}

function scaleBoxRects(
  rects: ReturnType<typeof computeBoxRects>,
  scaleX: number,
  scaleY: number,
) {
  const scaleRect = (rect: { left: number; top: number; width: number; height: number }) => ({
    left: rect.left * scaleX,
    top: rect.top * scaleY,
    width: rect.width * scaleX,
    height: rect.height * scaleY,
  });
  return {
    margin: scaleRect(rects.margin),
    border: scaleRect(rects.border),
    padding: scaleRect(rects.padding),
    content: scaleRect(rects.content),
  };
}

function RemoteRulers({
  rect,
}: {
  rect: { left: number; top: number; width: number; height: number };
}) {
  const centerX = rect.left + rect.width / 2;
  const centerY = rect.top + rect.height / 2;
  return (
    <>
      <div className="browser-remote-ruler horizontal" style={{ top: `${centerY}px`, left: '0', right: '0' }} />
      <div className="browser-remote-ruler vertical" style={{ left: `${centerX}px`, top: '0', bottom: '0' }} />
    </>
  );
}

function RemoteLabel({
  text,
  left,
  top,
}: {
  text: string;
  left: number;
  top: number;
}) {
  if (!text) return null;
  return (
    <span
      className="browser-remote-dim-label"
      style={{ left: `${left}px`, top: `${top}px` }}
    >
      {text}
    </span>
  );
}

function computeRemoteLabels(
  box: BoxModelLayers,
  rects: ReturnType<typeof computeBoxRects> | null,
) {
  if (!rects) return null;
  return {
    marginTop: box.margin.top > 0 ? `↑${Math.round(box.margin.top)}` : '',
    marginBottom: box.margin.bottom > 0 ? `↓${Math.round(box.margin.bottom)}` : '',
    marginLeft: box.margin.left > 0 ? `←${Math.round(box.margin.left)}` : '',
    marginRight: box.margin.right > 0 ? `→${Math.round(box.margin.right)}` : '',
    content: `${Math.round(rects.content.width)} x ${Math.round(rects.content.height)}`,
  };
}

function drawRulers(
  ctx: CanvasRenderingContext2D,
  rect: InspectRect,
  canvasW: number,
  canvasH: number,
) {
  const cx = rect.left + rect.width / 2;
  const cy = rect.top + rect.height / 2;

  ctx.save();
  ctx.strokeStyle = COLORS.ruler;
  ctx.lineWidth = 0.5;
  ctx.setLineDash([4, 4]);

  // Horizontal
  ctx.beginPath();
  ctx.moveTo(0, cy);
  ctx.lineTo(rect.left, cy);
  ctx.moveTo(rect.left + rect.width, cy);
  ctx.lineTo(canvasW, cy);
  ctx.stroke();

  // Vertical
  ctx.beginPath();
  ctx.moveTo(cx, 0);
  ctx.lineTo(cx, rect.top);
  ctx.moveTo(cx, rect.top + rect.height);
  ctx.lineTo(cx, canvasH);
  ctx.stroke();

  ctx.setLineDash([]);
  ctx.restore();
}

function drawDimensionLabels(
  ctx: CanvasRenderingContext2D,
  box: BoxModelLayers,
  tx: (x: number, y: number) => { x: number; y: number },
  _scaleX: number,
  _scaleY: number,
) {
  const c = box.contentRect;
  const m = box.margin;

  ctx.save();
  ctx.font = '10px SF Mono, Cascadia Code, Fira Code, monospace';
  ctx.textBaseline = 'middle';
  ctx.textAlign = 'center';

  // Margin top label
  if (m.top > 2) {
    const mid = tx(c.x + c.width / 2, c.y - m.top / 2);
    drawLabel(ctx, `↑${Math.round(m.top)}`, mid.x, mid.y);
  }
  // Margin bottom
  if (m.bottom > 2) {
    const mid = tx(c.x + c.width / 2, c.y + c.height + m.bottom / 2);
    drawLabel(ctx, `↓${Math.round(m.bottom)}`, mid.x, mid.y);
  }
  // Margin left
  if (m.left > 2) {
    const mid = tx(c.x - m.left / 2, c.y + c.height / 2);
    drawLabel(ctx, `←${Math.round(m.left)}`, mid.x, mid.y);
  }
  // Margin right
  if (m.right > 2) {
    const mid = tx(c.x + c.width + m.right / 2, c.y + c.height / 2);
    drawLabel(ctx, `→${Math.round(m.right)}`, mid.x, mid.y);
  }

  // Content dimensions
  const contentW = Math.round(c.width);
  const contentH = Math.round(c.height);
  const contentBottom = tx(c.x + c.width / 2, c.y + c.height + 2);

  if (contentW > 0 && contentH > 0) {
    drawLabel(ctx, `${contentW} × ${contentH}`, contentBottom.x, contentBottom.y + 10);
  }

  ctx.restore();
}

function drawLabel(ctx: CanvasRenderingContext2D, text: string, x: number, y: number) {
  const metrics = ctx.measureText(text);
  const pad = 3;
  const w = metrics.width + pad * 2;
  const h = 14;

  ctx.fillStyle = 'rgba(15, 23, 42, 0.85)';
  ctx.beginPath();
  ctx.roundRect(x - w / 2, y - h / 2, w, h, 3);
  ctx.fill();

  ctx.fillStyle = 'rgba(255, 255, 255, 0.9)';
  ctx.fillText(text, x, y);
}

/* ── Selected element highlight (persistent, after click) ── */

export function SelectedHighlight({
  selection,
  iframeRef,
}: {
  selection: BrowserElementSelection | null;
  iframeRef: React.RefObject<HTMLIFrameElement | null>;
  stageRef?: React.RefObject<HTMLDivElement | null>;
}) {
  const canvasRef = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    const shell = canvas?.parentElement;
    if (!canvas || !shell) return;

    const observer = new ResizeObserver(() => {
      const rect = shell.getBoundingClientRect();
      const dpr = window.devicePixelRatio || 1;
      canvas.width = Math.round(rect.width * dpr);
      canvas.height = Math.round(rect.height * dpr);
      canvas.style.width = `${rect.width}px`;
      canvas.style.height = `${rect.height}px`;
    });

    observer.observe(shell);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    const canvas = canvasRef.current;
    const iframe = iframeRef.current;
    if (!canvas || !iframe || !selection?.boundingRect || !selection?.boxModel) return;

    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    const dpr = window.devicePixelRatio || 1;
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.save();
    ctx.scale(dpr, dpr);

    // Canvas and iframe share the same parent (shell), so offset = 0
    const scaleX = canvas.clientWidth / Math.max(1, iframe.clientWidth);
    const scaleY = canvas.clientHeight / Math.max(1, iframe.clientHeight);

    const box = selection.boxModel;
    const c = box.contentRect;
    const p = box.padding;
    const b = box.border;

    const fullRect = {
      x: c.x - p.left - b.left,
      y: c.y - p.top - b.top,
      w: c.width + p.left + p.right + b.left + b.right,
      h: c.height + p.top + p.bottom + b.top + b.bottom,
    };

    const sx = fullRect.x * scaleX;
    const sy = fullRect.y * scaleY;
    const sw = fullRect.w * scaleX;
    const sh = fullRect.h * scaleY;

    // Blue selected highlight
    ctx.fillStyle = COLORS.selected;
    ctx.fillRect(sx, sy, sw, sh);
    ctx.strokeStyle = COLORS.selectedBorder;
    ctx.lineWidth = 2;
    ctx.strokeRect(sx + 1, sy + 1, sw - 2, sh - 2);

    // "Selected" badge
    ctx.font = 'bold 10px SF Mono, Cascadia Code, Fira Code, monospace';
    const badge = `✓ ${selection.tagName.toLowerCase()}`;
    const tm = ctx.measureText(badge);
    const bw = tm.width + 10;
    const bx = sx;
    const by = sy - 18;

    ctx.fillStyle = 'rgba(59, 130, 246, 0.9)';
    ctx.beginPath();
    ctx.roundRect(bx, by, bw, 16, 3);
    ctx.fill();

    ctx.fillStyle = '#ffffff';
    ctx.textBaseline = 'middle';
    ctx.fillText(badge, bx + 5, by + 8);

    ctx.restore();
  }, [selection, iframeRef]);

  if (!selection?.boundingRect) return null;

  return (
    <canvas
      ref={canvasRef}
      className="browser-inspect-canvas browser-selected-canvas"
      style={{ pointerEvents: 'none' }}
    />
  );
}

/* ── Mini Panel (Tier 4: Styles, Events, A11y) ── */

export function InspectMiniPanel({ selection, visible, onClose }: MiniPanelProps) {
  const [activeTab, setActiveTab] = useState<'styles' | 'events' | 'a11y'>('styles');

  if (!visible || !selection) return null;

  return (
    <div className="inspect-mini-panel">
      <div className="inspect-mini-header">
        <span className="inspect-mini-title">
          {selection.tagName.toLowerCase()}
          {selection.role ? ` [${selection.role}]` : ''}
        </span>
        <button
          type="button"
          className="inspect-mini-close"
          onClick={onClose}
          aria-label="Close panel"
        >
          ✕
        </button>
      </div>

      <div className="inspect-mini-tabs" role="tablist">
        <button
          type="button"
          role="tab"
          className={`inspect-mini-tab${activeTab === 'styles' ? ' active' : ''}`}
          onClick={() => setActiveTab('styles')}
        >
          Styles
        </button>
        <button
          type="button"
          role="tab"
          className={`inspect-mini-tab${activeTab === 'events' ? ' active' : ''}`}
          onClick={() => setActiveTab('events')}
        >
          Events
        </button>
        <button
          type="button"
          role="tab"
          className={`inspect-mini-tab${activeTab === 'a11y' ? ' active' : ''}`}
          onClick={() => setActiveTab('a11y')}
        >
          A11y
        </button>
      </div>

      <div className="inspect-mini-content">
        {activeTab === 'styles' && <StylesPanel selection={selection} />}
        {activeTab === 'events' && <EventsPanel selection={selection} />}
        {activeTab === 'a11y' && <A11yPanel selection={selection} />}
      </div>

      {selection.parentChain && selection.parentChain.length > 0 && (
        <div className="inspect-mini-parent-chain">
          <div className="inspect-mini-section-title">DOM Path</div>
          <div className="inspect-mini-path">
            {selection.parentChain.map((item, idx) => {
              const key = `${item.tagName}${item.id ?? ''}${item.classes.join(',')}`;
              return (
                <span key={key} className="inspect-path-segment">
                  {idx > 0 && <span className="inspect-path-sep">›</span>}
                  <span className="inspect-path-tag">{item.tagName}</span>
                  {item.id && <span className="inspect-path-id">#{item.id}</span>}
                  {item.classes.slice(0, 1).map((c) => (
                    <span key={c} className="inspect-path-class">.{c}</span>
                  ))}
                </span>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}

/* ── Styles sub-panel (Chrome DevTools style) ── */

function StylesPanel({ selection }: { selection: BrowserElementSelection }) {
  const cs = selection.computedStyle;
  const bm = selection.boxModel;

  return (
    <div className="inspect-styles">
      {/* Element header */}
      <div className="inspect-element-header">
        <span className="inspect-element-tag">{selection.tagName.toLowerCase()}</span>
        {selection.attributes?.id && <span className="inspect-element-id">#{selection.attributes.id}</span>}
        {selection.attributes?.class && (
          <span className="inspect-element-classes">
            {selection.attributes.class.split(/\s+/).filter(Boolean).map((c) => (
              <span key={c} className="inspect-element-class">.{c}</span>
            ))}
          </span>
        )}
      </div>

      {/* Box Model Diagram */}
      {bm && (
        <>
          <div className="inspect-mini-section-title">Box Model</div>
          <BoxModelDiagram box={bm} />
        </>
      )}

      {/* Computed Styles — grouped like Chrome DevTools */}
      {cs && (
        <>
          <div className="inspect-mini-section-title">Layout</div>
          <table className="inspect-styles-table">
            <tbody>
              <StyleRow label="display" value={cs.display} />
              <StyleRow label="position" value={cs.position} />
              {cs.position !== 'static' && (
                <>
                  {cs.top && cs.top !== 'auto' && <StyleRow label="top" value={cs.top} />}
                  {cs.left && cs.left !== 'auto' && <StyleRow label="left" value={cs.left} />}
                  {cs.right && cs.right !== 'auto' && <StyleRow label="right" value={cs.right} />}
                  {cs.bottom && cs.bottom !== 'auto' && <StyleRow label="bottom" value={cs.bottom} />}
                </>
              )}
              <StyleRow label="width" value={cs.width} />
              <StyleRow label="height" value={cs.height} />
              <StyleRow label="margin" value={cs.margin} />
              <StyleRow label="padding" value={cs.padding} />
              <StyleRow label="border" value={cs.border} />
              <StyleRow label="border-radius" value={cs.borderRadius} />
              {cs.flex && <StyleRow label="flex" value={cs.flex} />}
              {cs.gap && <StyleRow label="gap" value={cs.gap} />}
              <StyleRow label="overflow" value={cs.overflow} />
            </tbody>
          </table>

          <div className="inspect-mini-section-title">Typography</div>
          <table className="inspect-styles-table">
            <tbody>
              <StyleRow label="font" value={`${cs.fontSize} ${cs.fontWeight}`} />
              <StyleRow label="font-family" value={cs.fontFamily} />
              <StyleRow label="line-height" value={cs.lineHeight} />
              <StyleRow label="text-align" value={cs.textAlign} />
              <StyleRow label="color" value={cs.color} color={cs.color} />
              <StyleRow label="background" value={cs.backgroundColor} color={cs.backgroundColor} />
            </tbody>
          </table>

          <div className="inspect-mini-section-title">Visibility</div>
          <table className="inspect-styles-table">
            <tbody>
              <StyleRow label="opacity" value={cs.opacity} />
              <StyleRow label="visibility" value={cs.visibility} />
              <StyleRow label="z-index" value={cs.zIndex} />
            </tbody>
          </table>
        </>
      )}

      {/* Attributes */}
      {selection.attributes && Object.keys(selection.attributes).length > 0 && (
        <>
          <div className="inspect-mini-section-title">Attributes</div>
          <div className="inspect-attrs-list">
            {Object.entries(selection.attributes).map(([k, v]) => (
              <div key={k} className="inspect-attr-row">
                <span className="inspect-attr-key">{k}</span>
                <span className="inspect-attr-val" title={v}>{v || '""'}</span>
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

function StyleRow({ label, value, color }: { label: string; value: string; color?: string }) {
  return (
    <tr className="inspect-style-row">
      <td className="inspect-style-prop">{label}</td>
      <td className="inspect-style-val">
        {color && <span className="inspect-color-swatch" style={{ backgroundColor: color }} />}
        {value}
      </td>
    </tr>
  );
}

/* ── Box Model Diagram (Chrome DevTools style) ── */

function BoxModelDiagram({ box }: { box: BoxModelLayers }) {
  const m = box.margin;
  const b = box.border;
  const p = box.padding;
  const c = box.contentRect;

  return (
    <div className="inspect-boxmodel">
      <div className="boxmodel-outer" title="Margin">
        <span className="boxmodel-label margin-label">
          {m.top || m.right || m.bottom || m.left ? `${Math.round(m.top)} ${Math.round(m.right)} ${Math.round(m.bottom)} ${Math.round(m.left)}` : '—'}
        </span>
        <div className="boxmodel-border" title="Border">
          <span className="boxmodel-label border-label">
            {b.top || b.right || b.bottom || b.left ? `${Math.round(b.top)} ${Math.round(b.right)} ${Math.round(b.bottom)} ${Math.round(b.left)}` : '—'}
          </span>
          <div className="boxmodel-padding" title="Padding">
            <span className="boxmodel-label padding-label">
              {p.top || p.right || p.bottom || p.left ? `${Math.round(p.top)} ${Math.round(p.right)} ${Math.round(p.bottom)} ${Math.round(p.left)}` : '—'}
            </span>
            <div className="boxmodel-content" title="Content">
              <span className="boxmodel-label content-label">
                {Math.round(c.width)} × {Math.round(c.height)}
              </span>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

/* ── Events sub-panel ── */

function EventsPanel({ selection }: { selection: BrowserElementSelection }) {
  const listeners = selection.eventListeners;

  return (
    <div className="inspect-events">
      {listeners && listeners.length > 0 ? (
        <div className="inspect-events-list">
          {listeners.map((l) => (
            <div key={l.type} className="inspect-event-row">
              <span className="inspect-event-type">{l.type}</span>
              <code className="inspect-event-handler">{l.handlerBody}</code>
            </div>
          ))}
        </div>
      ) : (
        <div className="inspect-empty">
          No inline event handlers detected.
          <br />
          <span className="inspect-note">addEventListener handlers cannot be detected from outside.</span>
        </div>
      )}
    </div>
  );
}

/* ── Accessibility sub-panel ── */

function A11yPanel({ selection }: { selection: BrowserElementSelection }) {
  const a11y = selection.accessibilityInfo;

  return (
    <div className="inspect-a11y">
      {a11y ? (
        <table className="inspect-styles-table">
          <tbody>
            <StyleRow label="role" value={a11y.role || '(none)'} />
            <StyleRow label="label" value={a11y.label || '(none)'} />
            <StyleRow label="tabIndex" value={a11y.tabIndex !== undefined ? String(a11y.tabIndex) : '(none)'} />
            <StyleRow label="focusable" value={a11y.focusable ? '✓ Yes' : '✗ No'} />
            {a11y.contrastRatio && (
              <StyleRow label="contrast" value={a11y.contrastRatio} />
            )}
          </tbody>
        </table>
      ) : (
        <div className="inspect-empty">No accessibility info available.</div>
      )}
    </div>
  );
}
