import { useState } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { useChatStore } from '../../stores/chat';
import { useWorkspaceStore } from '../../stores/workspace';
import { getTerminalHandle } from '../../terminalRegistry';
import { isShellLanguageCompatible } from '../../terminalIntegration';
import type { ChatMessage as ChatMessageType } from '../../types';

type ChatMessageProps = {
  message: ChatMessageType;
  streaming?: boolean;
};

function toolKindIcon(kind: string): string {
  switch (kind) {
    case 'read': return '📄';
    case 'edit': return '✏️';
    case 'delete': return '🗑️';
    case 'move': return '📦';
    case 'search': return '🔍';
    case 'execute': return '▶️';
    case 'think': return '💭';
    case 'fetch': return '🌐';
    default: return '⚙️';
  }
}

function ToolCallStatusIcon({ status }: { status: string }) {
  switch (status) {
    case 'completed': return <span className="tool-status tool-status-done">✓</span>;
    case 'failed': return <span className="tool-status tool-status-fail">✗</span>;
    case 'in_progress': return <span className="tool-status tool-status-running">⟳</span>;
    default: return <span className="tool-status tool-status-pending">○</span>;
  }
}

/** Detect if language is terminal-runnable */
/** Inline code — no run button */
function MarkdownCode({ className, children, ...props }: React.ComponentProps<'code'> & { node?: unknown }) {
  return <code className={className} {...props}>{children}</code>;
}

/** Block code (inside <pre>) — add "Run in terminal" button for shell languages */
function MarkdownPre({ children }: React.ComponentProps<'pre'> & { node?: unknown }) {
  const [ran, setRan] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const activeTerminalId = useChatStore((s) => s.activeTerminalId);
  const autoApprove = useChatStore((s) => s.autoApprove);
  const terminalHandle = activeTerminalId ? getTerminalHandle(activeTerminalId) : null;

  // Extract code text and language from ReactMarkdown children
  let lang: string | undefined;
  let codeText = '';
  const child = children as React.ReactElement<React.HTMLAttributes<HTMLElement>> | undefined;
  if (child?.props) {
    const cls = (child.props.className as string) || '';
    const m = cls.match(/language-(\w+)/);
    if (m) lang = m[1];
    codeText = String(child.props.children ?? '');
  }

  const canRun = !!terminalHandle && isShellLanguageCompatible(lang, terminalHandle.shellType);

  const executeRun = () => {
    if (!terminalHandle) return;
    // Strip leading `$ ` or `> ` prompts from each line
    const cleaned = codeText
      .split('\n')
      .map((l) => l.replace(/^\$\s*|^>\s*/, ''))
      .join('\n')
      .trim();
    // Reveal terminal panel before sending the command.
    useWorkspaceStore.getState().showTerminal();
    terminalHandle.sendCommand(cleaned);
    setRan(true);
    setConfirming(false);
    setTimeout(() => setRan(false), 2000);
  };

  const handleClick = () => {
    if (autoApprove) {
      executeRun();
    } else if (confirming) {
      executeRun();
    } else {
      setConfirming(true);
      // Auto-cancel confirmation after 5s
      setTimeout(() => setConfirming(false), 5000);
    }
  };

  const label = ran ? '✓ Sent' : confirming ? '⚠ Confirm?' : '▶ Run';

  return (
    <div className="chat-code-block-wrapper">
      <pre className="chat-code-block">{children}</pre>
      {canRun && (
        <button
          type="button"
          className={`chat-code-run-btn${ran ? ' ran' : ''}${confirming ? ' confirm' : ''}`}
          onClick={handleClick}
          title={confirming ? 'Click again to confirm execution' : 'Run in active terminal'}
        >
          {label}
        </button>
      )}
    </div>
  );
}

export function ChatMessage({ message, streaming }: ChatMessageProps) {
  const [contextExpanded, setContextExpanded] = useState(false);
  const [thinkingExpanded, setThinkingExpanded] = useState(false);

  if (message.role === 'tool_call' && message.toolCall) {
    return (
      <div className="chat-entry-tool">
        <ToolCallCard tc={message.toolCall} />
        {message.diffs && message.diffs.length > 0 && (
          <div className="chat-message-diffs">
            {message.diffs.map((diff, i) => (
              <div key={i} className="diff-block">
                <div className="diff-header">{diff.path}</div>
                <pre className="diff-content">
                  <code>{formatDiff(diff.oldText, diff.newText)}</code>
                </pre>
              </div>
            ))}
          </div>
        )}
      </div>
    );
  }

  if (message.role === 'plan' && message.plan) {
    return (
      <div className="chat-entry-plan">
        <PlanBlock entries={message.plan} />
      </div>
    );
  }

  return (
    <div className={`chat-message ${message.role}`}>
      {message.context && (
        <div className="chat-message-context" onClick={() => setContextExpanded(!contextExpanded)}>
          <div className="chat-message-context-header">
            {message.context.filePath}:{message.context.startLine}-{message.context.endLine}
          </div>
          {contextExpanded && (
            <pre className="chat-message-code">{message.context.selectedCode}</pre>
          )}
        </div>
      )}

      {message.thinking && (
        <div className="chat-message-thinking" onClick={() => setThinkingExpanded(!thinkingExpanded)}>
          <div className="chat-message-thinking-header">
            💭 Thinking {thinkingExpanded ? '▾' : '▸'}
          </div>
          {thinkingExpanded && (
            <div className="chat-message-thinking-content">{message.thinking}</div>
          )}
        </div>
      )}

      <div className="chat-message-bubble chat-markdown">
        {message.content ? (
          streaming ? (
            <pre className="chat-streaming-text">{message.content}</pre>
          ) : (
            <ReactMarkdown
              remarkPlugins={[remarkGfm]}
              components={{ code: MarkdownCode, pre: MarkdownPre }}
            >{message.content}</ReactMarkdown>
          )
        ) : message.role === 'assistant' ? (
          <span className="chat-typing-indicator"><span /><span /><span /></span>
        ) : null}
      </div>

      {message.diffs && message.diffs.length > 0 && (
        <div className="chat-message-diffs">
          {message.diffs.map((diff, i) => (
            <div key={i} className="diff-block">
              <div className="diff-header">{diff.path}</div>
              <pre className="diff-content">
                <code>{formatDiff(diff.oldText, diff.newText)}</code>
              </pre>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function PlanBlock({ entries }: { entries: import('../../types').PlanEntryInfo[] }) {
  const [expanded, setExpanded] = useState(true);
  const completed = entries.filter((e) => e.status === 'completed').length;
  const inProgress = entries.filter((e) => e.status === 'in_progress').length;
  const total = entries.length;
  const allDone = completed === total;

  const statusLabel = allDone
    ? 'All Done'
    : inProgress > 0
      ? `${completed}/${total}`
      : `${total} Tasks`;

  return (
    <div className={`chat-plan-block ${allDone ? 'plan-done' : ''}`}>
      <div className="chat-plan-header" onClick={() => setExpanded(!expanded)}>
        <span className="chat-plan-toggle">{expanded ? '▾' : '▸'}</span>
        <span className="chat-plan-label">Plan</span>
        <span className="chat-plan-status">{statusLabel}</span>
      </div>
      {expanded && (
        <div className="chat-plan-entries">
          {entries.map((entry, i) => (
            <div key={i} className={`chat-plan-entry chat-plan-entry-${entry.status ?? 'pending'}`}>
              <span className="chat-plan-entry-icon">
                {entry.status === 'completed' ? '✓' : entry.status === 'in_progress' ? '⟳' : '○'}
              </span>
              <span className={entry.status === 'completed' ? 'chat-plan-entry-done-text' : ''}>{entry.content}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

function ToolCallCard({ tc }: { tc: import('../../types').ToolCallInfo }) {
  const [expanded, setExpanded] = useState(false);
  const hasDetails = !!(tc.content || (tc.locations && tc.locations.length > 0));
  const fileName = tc.locations?.[0]?.path?.split(/[/\\]/).pop();

  return (
    <div className={`tool-call tool-call-${tc.status}`} onClick={() => hasDetails && setExpanded(!expanded)}>
      <div className="tool-call-header">
        <ToolCallStatusIcon status={tc.status} />
        <span className="tool-call-icon">{toolKindIcon(tc.kind)}</span>
        <span className="tool-call-title">{tc.title}</span>
        {fileName && <span className="tool-call-file">{fileName}</span>}
        {hasDetails && <span className="tool-call-expand">{expanded ? '▾' : '▸'}</span>}
      </div>
      {expanded && (
        <div className="tool-call-details">
          {tc.locations && tc.locations.map((loc, i) => (
            <div key={i} className="tool-call-location">
              {loc.path}{loc.line ? `:${loc.line}` : ''}
            </div>
          ))}
          {tc.content && <pre className="tool-call-output">{tc.content}</pre>}
        </div>
      )}
    </div>
  );
}

function formatDiff(oldText: string, newText: string): string {
  const oldLines = oldText.split('\n');
  const newLines = newText.split('\n');
  const result: string[] = [];

  const maxLen = Math.max(oldLines.length, newLines.length);
  let oi = 0;
  let ni = 0;

  while (oi < oldLines.length || ni < newLines.length) {
    if (oi < oldLines.length && ni < newLines.length && oldLines[oi] === newLines[ni]) {
      result.push(`  ${oldLines[oi]}`);
      oi++;
      ni++;
    } else if (oi < oldLines.length && (ni >= newLines.length || !newLines.includes(oldLines[oi]))) {
      result.push(`- ${oldLines[oi]}`);
      oi++;
    } else if (ni < newLines.length) {
      result.push(`+ ${newLines[ni]}`);
      ni++;
    }

    if (result.length > maxLen * 2) break;
  }

  return result.join('\n');
}
