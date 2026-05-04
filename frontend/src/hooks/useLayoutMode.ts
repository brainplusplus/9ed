import { useEffect, useState } from 'react';

export type LayoutMode = 'desktop' | 'tablet' | 'mobile';

const DESKTOP_QUERY = '(min-width: 1024px)';
const TABLET_QUERY = '(min-width: 768px) and (max-width: 1023px)';

function getLayoutMode(): LayoutMode {
  if (window.matchMedia(DESKTOP_QUERY).matches) return 'desktop';
  if (window.matchMedia(TABLET_QUERY).matches) return 'tablet';
  return 'mobile';
}

export function useLayoutMode(): LayoutMode {
  const [mode, setMode] = useState<LayoutMode>(getLayoutMode);

  useEffect(() => {
    const desktopMql = window.matchMedia(DESKTOP_QUERY);
    const tabletMql = window.matchMedia(TABLET_QUERY);

    function update() {
      setMode(getLayoutMode());
    }

    desktopMql.addEventListener('change', update);
    tabletMql.addEventListener('change', update);

    return () => {
      desktopMql.removeEventListener('change', update);
      tabletMql.removeEventListener('change', update);
    };
  }, []);

  return mode;
}
