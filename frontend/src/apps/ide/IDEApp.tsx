import { useWorkspaceStore } from '../../stores/workspace';
import { ProjectPicker } from './ProjectPicker';
import { IDEWorkspace } from './IDEWorkspace';

export function IDEApp() {
  const activeProjectId = useWorkspaceStore((s) => s.activeProjectId);
  const showPicker = useWorkspaceStore((s) => s.showPicker);

  if (!activeProjectId || showPicker) {
    return <ProjectPicker />;
  }

  return <IDEWorkspace />;
}
