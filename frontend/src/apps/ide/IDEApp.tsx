import { useEffect, useRef } from 'react';
import { useWorkspaceStore } from '../../stores/workspace';
import { getRecentProjects } from '../../api';
import { ProjectPicker } from './ProjectPicker';
import { IDEWorkspace } from './IDEWorkspace';

export function IDEApp() {
  const activeProjectId = useWorkspaceStore((s) => s.activeProjectId);
  const showPicker = useWorkspaceStore((s) => s.showPicker);
  const addProject = useWorkspaceStore((s) => s.addProject);
  const autoOpenedRef = useRef(false);

  useEffect(() => {
    if (autoOpenedRef.current || activeProjectId) return;
    autoOpenedRef.current = true;

    void getRecentProjects().then((projects) => {
      if (projects.length > 0 && !useWorkspaceStore.getState().activeProjectId) {
        const last = projects[0];
        addProject(last.path, last.name);
      }
    }).catch(() => {});
  }, [activeProjectId, addProject]);

  if (!activeProjectId || showPicker) {
    return <ProjectPicker />;
  }

  return <IDEWorkspace />;
}
