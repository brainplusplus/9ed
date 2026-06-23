import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { ChatInput } from './ChatInput';
import { useChatStore } from '../../stores/chat';
import { useWorkspaceStore } from '../../stores/workspace';

Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });

const getGitFiles = vi.fn();

vi.mock('../../api', () => ({
  getGitFiles: (...args: unknown[]) => getGitFiles(...args),
}));

function resetStores() {
  useChatStore.setState({
    sessions: [],
    activeSessionId: 'session-lock',
    agents: [],
    chatVisible: false,
    historySessions: [],
    historyLoaded: false,
    historyWorkDir: null,
    queuedMessages: {},
    includeIgnoredInMentions: false,
    autoApprove: false,
    useActiveBrowser: false,
    browserSelectionMode: 'detail',
    useActiveTerminal: false,
    activeTerminalId: null,
    browserSelection: null,
    browserSelectionCapture: null,
    restoring: false,
    lastRestoreError: null,
  });
  useWorkspaceStore.setState({
    projects: [{
      id: 'project-1',
      path: '/repo',
      name: 'repo',
      openFiles: [],
      activeFileId: null,
      terminalTabs: [],
      activeTerminalTabId: null,
      terminalSessions: [],
    }],
    activeProjectId: 'project-1',
    activePanel: 'explorer',
    sidebarVisible: true,
    terminalVisible: true,
    chatVisible: true,
    showPicker: false,
  });
}

describe('ChatInput input locked (ADR-0005 VAL-PTY-007)', () => {
  let container: HTMLDivElement;
  let root: Root;

  beforeEach(() => {
    vi.clearAllMocks();
    resetStores();
    getGitFiles.mockResolvedValue([]);
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => {
      root.unmount();
    });
    container.remove();
  });

  it('disables the textarea and shows "Input locked by {holder}" when lockedBy is set', () => {
    act(() => {
      root.render(
        <ChatInput
          onSend={vi.fn()}
          onCancel={vi.fn()}
          streaming={false}
          disabled={false}
          canSend
          lockedBy="client-A"
        />,
      );
    });

    const textarea = container.querySelector('textarea.chat-textarea') as HTMLTextAreaElement;
    expect(textarea).not.toBeNull();
    expect(textarea.disabled).toBe(true);

    expect(container.textContent).toContain('Input locked by client-A');
  });

  it('does not disable the textarea or show the lock banner when lockedBy is undefined', () => {
    act(() => {
      root.render(
        <ChatInput
          onSend={vi.fn()}
          onCancel={vi.fn()}
          streaming={false}
          disabled={false}
          canSend
        />,
      );
    });

    const textarea = container.querySelector('textarea.chat-textarea') as HTMLTextAreaElement;
    expect(textarea).not.toBeNull();
    expect(textarea.disabled).toBe(false);
    expect(container.textContent).not.toContain('Input locked by');
  });

  it('does not call onSend when locked and user presses Enter', () => {
    const onSend = vi.fn();
    act(() => {
      root.render(
        <ChatInput
          onSend={onSend}
          onCancel={vi.fn()}
          streaming={false}
          disabled={false}
          canSend
          lockedBy="client-B"
        />,
      );
    });

    const textarea = container.querySelector('textarea.chat-textarea') as HTMLTextAreaElement;
    act(() => {
      // React-controlled input: set value via native setter + dispatch input
      const setter = Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, 'value')!.set!;
      setter.call(textarea, 'hello world');
      textarea.dispatchEvent(new Event('input', { bubbles: true }));
    });

    act(() => {
      textarea.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }),
      );
    });

    expect(onSend).not.toHaveBeenCalled();
  });
});
