const KEYBOARD_DELTA_PX = 160;

function viewportSize() {
  const visualViewport = window.visualViewport;
  const doc = document.documentElement;
  const layoutHeight = Math.max(window.innerHeight, doc.clientHeight);
  const visualHeight = visualViewport?.height ?? layoutHeight;
  const keyboardOpen = layoutHeight - visualHeight > KEYBOARD_DELTA_PX;

  return {
    height: keyboardOpen ? visualHeight : Math.max(layoutHeight, visualHeight),
    width: visualViewport?.width ?? window.innerWidth,
    top: keyboardOpen ? (visualViewport?.offsetTop ?? 0) : 0,
    left: visualViewport?.offsetLeft ?? 0,
  };
}

export function installViewportVars() {
  const applyViewportVars = () => {
    const { height, width, top, left } = viewportSize();
    const root = document.documentElement;

    root.style.setProperty('--app-viewport-height', `${height}px`);
    root.style.setProperty('--app-viewport-width', `${width}px`);
    root.style.setProperty('--app-viewport-top', `${top}px`);
    root.style.setProperty('--app-viewport-left', `${left}px`);
  };

  applyViewportVars();

  window.addEventListener('resize', applyViewportVars);
  window.addEventListener('orientationchange', applyViewportVars);
  window.visualViewport?.addEventListener('resize', applyViewportVars);
  window.visualViewport?.addEventListener('scroll', applyViewportVars);
}
