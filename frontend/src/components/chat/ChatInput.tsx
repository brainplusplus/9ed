import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { getGitFiles } from '../../api';
import type { GitRepoFile } from '../../api';
import { useChatStore } from '../../stores/chat';
import type { QueuedMessage } from '../../stores/chat';
import { useWorkspaceStore } from '../../stores/workspace';
import type { BrowserElementSelection, DirEntry } from '../../types';

type ChatInputProps = {
  onSend: (content: string, attachments?: Attachment[]) => void | Promise<void>;
  onCancel: () => void;
  streaming: boolean;
  disabled: boolean;
  canSend?: boolean;
};

export type Attachment = {
  type: 'file' | 'image';
  path: string;
  name: string;
  previewUrl?: string;
};

type SpeechResultItem = {
  transcript: string;
};

type SpeechResultList = ArrayLike<SpeechResultItem> & {
  isFinal: boolean;
};

type SpeechRecognitionLike = {
  continuous: boolean;
  interimResults: boolean;
  lang: string;
  start: () => void;
  stop: () => void;
  onresult: ((event: { results: ArrayLike<SpeechResultList> }) => void) | null;
  onerror: ((event: { error?: string }) => void) | null;
  onend: (() => void) | null;
};

function MicIcon() {
  return (
    <svg className="chat-action-icon" viewBox="0 0 20 20" aria-hidden="true" focusable="false">
      <path d="M10 3.25a2.25 2.25 0 0 0-2.25 2.25v4a2.25 2.25 0 0 0 4.5 0v-4A2.25 2.25 0 0 0 10 3.25Z" />
      <path d="M5.75 9.25v.25a4.25 4.25 0 0 0 8.5 0v-.25" />
      <path d="M10 13.75v2.5" />
      <path d="M7.75 16.25h4.5" />
    </svg>
  );
}

function getSpeechRecognitionCtor(): (new () => SpeechRecognitionLike) | null {
  const speechWindow = window as Window & {
    SpeechRecognition?: new () => SpeechRecognitionLike;
    webkitSpeechRecognition?: new () => SpeechRecognitionLike;
  };
  return speechWindow.SpeechRecognition ?? speechWindow.webkitSpeechRecognition ?? null;
}

async function requestMicrophoneAccess(): Promise<{ ok: boolean; blocked: boolean; message?: string }> {
  if (!navigator.mediaDevices?.getUserMedia) {
    return { ok: true, blocked: false };
  }

  try {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    stream.getTracks().forEach((track) => track.stop());
    return { ok: true, blocked: false };
  } catch (err) {
    const name = err instanceof DOMException ? err.name : '';
    const blocked = name === 'NotAllowedError' || name === 'PermissionDeniedError';
    return {
      ok: false,
      blocked,
      message: blocked
        ? 'Microphone permission is blocked. Allow it from the address bar, then click the mic again.'
        : 'Microphone permission could not be requested.',
    };
  }
}

export function ChatInput({ onSend, onCancel, streaming, disabled, canSend = true }: ChatInputProps) {
  const [value, setValue] = useState('');
  const [selectedIdx, setSelectedIdx] = useState(0);
  const [attachments, setAttachments] = useState<Attachment[]>([]);
  const [mentionQuery, setMentionQuery] = useState<string | null>(null);
  const [mentionResults, setMentionResults] = useState<DirEntry[]>([]);
  const [mentionIdx, setMentionIdx] = useState(0);
  const [voiceSupported, setVoiceSupported] = useState(false);
  const [voiceActive, setVoiceActive] = useState(false);
  const [voiceStatus, setVoiceStatus] = useState<string | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const speechRef = useRef<SpeechRecognitionLike | null>(null);
  const voiceBaseRef = useRef(''); // text before voice session started
  const voiceSessionRef = useRef(0);
  const voiceStartingRef = useRef(false);
  const valueRef = useRef('');

  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const commands = useChatStore((s) => {
    const session = s.sessions.find((sess) => sess.id === s.activeSessionId);
    return session?.commands;
  }) ?? [];
  const browserSelection = useChatStore((s) => s.browserSelection);
  const browserSelectionMode = useChatStore((s) => s.browserSelectionMode);
  const browserSelectionCapture = useChatStore((s) => s.browserSelectionCapture);
  const useActiveBrowser = useChatStore((s) => s.useActiveBrowser);
  const setBrowserSelection = useChatStore((s) => s.setBrowserSelection);
  const includeIgnored = useChatStore((s) => s.includeIgnoredInMentions);
  const enqueueMessage = useChatStore((s) => s.enqueueMessage);
  const projects = useWorkspaceStore((s) => s.projects);
  const activeProjectId = useWorkspaceStore((s) => s.activeProjectId);
  const projectPath = projects.find((p) => p.id === activeProjectId)?.path ?? '';

  const showCommands = value.startsWith('/') && !value.includes(' ') && commands.length > 0;
  const filteredCommands = useMemo(() => {
    if (!showCommands) return [];
    const query = value.slice(1).toLowerCase();
    return commands.filter((c) => c.name.toLowerCase().startsWith(query));
  }, [showCommands, value, commands]);

  const [mentionFiles, setMentionFiles] = useState<GitRepoFile[]>([]);
  const mentionFilesLoaded = useRef(false);
  const lastMentionProject = useRef('');
  const lastMentionIncludeIgnored = useRef(false);

  // Keep valueRef in sync with value state for use in callbacks
  useEffect(() => {
    valueRef.current = value;
  }, [value]);

  useEffect(() => {
    setVoiceSupported(Boolean(getSpeechRecognitionCtor()));
  }, []);

  useEffect(() => {
    return () => {
      voiceSessionRef.current += 1;
      speechRef.current?.stop();
      speechRef.current = null;
    };
  }, []);

  useEffect(() => {
    if (!projectPath) return;
    if (mentionFilesLoaded.current && lastMentionProject.current === projectPath && lastMentionIncludeIgnored.current === includeIgnored) return;
    let cancelled = false;
    getGitFiles(projectPath, includeIgnored).then((files) => {
      if (cancelled) return;
      setMentionFiles(files);
      mentionFilesLoaded.current = true;
      lastMentionProject.current = projectPath;
      lastMentionIncludeIgnored.current = includeIgnored;
    }).catch(() => {});
    return () => { cancelled = true; };
  }, [projectPath, includeIgnored]);

  useEffect(() => {
    if (mentionQuery === null) {
      setMentionResults([]);
      return;
    }
    const q = mentionQuery.toLowerCase();
    const filtered = mentionFiles
      .filter((f) => f.path.toLowerCase().includes(q))
      .slice(0, 12)
      .map((f): DirEntry => ({
        name: f.path,
        type: 'file',
        size: 0,
        modified: 0,
        ignored: f.ignored,
      }));
    setMentionResults(filtered);
    setMentionIdx(0);
  }, [mentionQuery, mentionFiles]);

  const handleMentionDetect = useCallback((text: string) => {
    const atIdx = text.lastIndexOf('@');
    if (atIdx >= 0 && (atIdx === 0 || text[atIdx - 1] === ' ' || text[atIdx - 1] === '\n')) {
      const after = text.slice(atIdx + 1);
      if (!after.includes(' ') && !after.includes('\n')) {
        setMentionQuery(after);
        return;
      }
    }
    setMentionQuery(null);
  }, []);

  const selectMention = useCallback((entry: DirEntry) => {
    const atIdx = value.lastIndexOf('@');
    const before = value.slice(0, atIdx);
    const sep = projectPath.includes('\\') ? '\\' : '/';
    const relativePath = entry.name;
    const fullPath = projectPath + sep + relativePath.replace(/\//g, sep);
    setAttachments((prev) => [...prev, { type: 'file', path: fullPath, name: relativePath }]);
    setValue(before);
    setMentionQuery(null);
    textareaRef.current?.focus();
  }, [projectPath, value]);

  const removeAttachment = useCallback((idx: number) => {
    setAttachments((prev) => prev.filter((_, i) => i !== idx));
  }, []);

  const handlePaste = useCallback((e: React.ClipboardEvent) => {
    const items = e.clipboardData.items;
    for (let i = 0; i < items.length; i++) {
      if (items[i].type.startsWith('image/')) {
        e.preventDefault();
        const file = items[i].getAsFile();
        if (file) {
          const name = `pasted-image-${Date.now()}.png`;
          const previewUrl = URL.createObjectURL(file);
          setAttachments((prev) => [...prev, { type: 'image', path: previewUrl, previewUrl, name }]);
        }
        return;
      }
    }
  }, []);

  const adjustHeight = useCallback(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${Math.min(el.scrollHeight, 160)}px`;
  }, []);

  const stopVoiceInput = useCallback((status: string | null = 'Stopping voice input...') => {
    voiceSessionRef.current += 1;
    const recognition = speechRef.current;
    speechRef.current = null;
    if (recognition) {
      recognition.onresult = null;
      recognition.onerror = null;
      recognition.onend = null;
      try {
        recognition.stop();
      } catch {}
    }
    setVoiceActive(false);
    setVoiceStatus(status);
  }, []);

  const handleSend = useCallback(() => {
    const trimmed = value.trim();
    if ((!trimmed && attachments.length === 0) || disabled || !canSend) return;

    if (voiceActive) {
      stopVoiceInput(null);
    }

    if (streaming && activeSessionId) {
      const queuedMsg: QueuedMessage = {
        id: Date.now().toString(36) + Math.random().toString(36).slice(2, 6),
        content: trimmed,
        attachments: attachments.length > 0 ? attachments : undefined,
        createdAt: Date.now(),
      };
      enqueueMessage(activeSessionId, queuedMsg);
    } else {
      void onSend(trimmed, attachments.length > 0 ? attachments : undefined);
    }

    setValue('');
    setAttachments([]);
    setSelectedIdx(0);
    setMentionQuery(null);
    if (textareaRef.current) {
      textareaRef.current.style.height = 'auto';
    }
  }, [activeSessionId, attachments, canSend, disabled, enqueueMessage, onSend, stopVoiceInput, streaming, value, voiceActive]);

  const toggleVoiceInput = useCallback(async () => {
    const SpeechCtor = getSpeechRecognitionCtor();
    if (!SpeechCtor || disabled) {
      setVoiceStatus('Voice input is unavailable in this browser.');
      return;
    }

    if (voiceActive) {
      stopVoiceInput();
      return;
    }

    if (voiceStartingRef.current) {
      return;
    }

    const requestId = voiceSessionRef.current + 1;
    voiceSessionRef.current = requestId;
    voiceStartingRef.current = true;
    setVoiceStatus('Requesting microphone permission...');
    const permission = await requestMicrophoneAccess();
    voiceStartingRef.current = false;

    if (voiceSessionRef.current !== requestId || disabled) {
      return;
    }

    if (!permission.ok) {
      setVoiceActive(false);
      setVoiceStatus(permission.message ?? 'Microphone permission was not granted.');
      return;
    }

    const recognition = new SpeechCtor();
    const sessionId = requestId;
    recognition.continuous = true;
    recognition.interimResults = true;
    recognition.lang = 'id-ID';
    speechRef.current = recognition;

    // Snapshot current text as the base; voice transcript appends after this
    voiceBaseRef.current = valueRef.current;

    setVoiceActive(true);
    setVoiceStatus('Listening...');

    recognition.onresult = (event) => {
      if (speechRef.current !== recognition || voiceSessionRef.current !== sessionId) {
        return;
      }

      // Build transcript from ALL results (committed final + current interim)
      // This avoids duplication: we replace the voice portion entirely each time
      let committed = '';
      let interim = '';
      for (let i = 0; i < event.results.length; i++) {
        const result = event.results[i];
        const text = result?.[0]?.transcript ?? '';
        if (result.isFinal) {
          committed += text;
        } else {
          interim += text;
        }
      }

      const transcript = (committed + interim).trim();
      if (!transcript) {
        return;
      }

      const base = voiceBaseRef.current;
      const next = base.length > 0 ? `${base} ${transcript}` : transcript;
      setValue(next);
      queueMicrotask(() => adjustHeight());
    };

    recognition.onerror = (event) => {
      if (speechRef.current !== recognition || voiceSessionRef.current !== sessionId) {
        return;
      }
      setVoiceActive(false);
      speechRef.current = null;
      setVoiceStatus(event.error ? `Voice input error: ${event.error}` : 'Voice input failed.');
    };

    recognition.onend = () => {
      if (speechRef.current !== recognition || voiceSessionRef.current !== sessionId) {
        return;
      }
      // On end, commit whatever was interim by rebuilding from final results only
      // (interim results become final or are lost — browser handles this)
      setVoiceActive(false);
      speechRef.current = null;
      setVoiceStatus((current) => (current === 'Listening...' || current === 'Stopping voice input...' ? null : current));
    };

    try {
      recognition.start();
    } catch {
      if (speechRef.current === recognition) {
        speechRef.current = null;
      }
      setVoiceActive(false);
      setVoiceStatus('Voice input could not start.');
    }
  }, [adjustHeight, disabled, stopVoiceInput, voiceActive]);

  const selectCommand = useCallback((name: string) => {
    setValue(`/${name} `);
    setSelectedIdx(0);
    textareaRef.current?.focus();
  }, []);

  const handleKeyDown = useCallback((e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    const isPlainEnter = (e.key === 'Enter' || e.code === 'Enter' || e.keyCode === 13) && !e.shiftKey;
    if (mentionResults.length > 0 && mentionQuery !== null) {
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        setMentionIdx((i) => Math.min(i + 1, mentionResults.length - 1));
        return;
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault();
        setMentionIdx((i) => Math.max(i - 1, 0));
        return;
      }
      if (e.key === 'Tab' || isPlainEnter) {
        e.preventDefault();
        selectMention(mentionResults[mentionIdx]);
        return;
      }
      if (e.key === 'Escape') {
        setMentionQuery(null);
        return;
      }
    } else if (filteredCommands.length > 0) {
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        setSelectedIdx((i) => Math.min(i + 1, filteredCommands.length - 1));
        return;
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault();
        setSelectedIdx((i) => Math.max(i - 1, 0));
        return;
      }
      if (e.key === 'Tab' || isPlainEnter) {
        e.preventDefault();
        selectCommand(filteredCommands[selectedIdx].name);
        return;
      }
      if (e.key === 'Escape') {
        setValue('');
        return;
      }
    } else if (isPlainEnter) {
      e.preventDefault();
      handleSend();
    }
  }, [filteredCommands, handleSend, mentionIdx, mentionQuery, mentionResults, selectCommand, selectMention, selectedIdx]);

  return (
    <div className="chat-input-area">
      {browserSelection && (
        <div className="chat-browser-selection">
          <div className="chat-browser-selection-copy">
            <span className="chat-browser-selection-label">Selected element</span>
            <span className="chat-browser-selection-mode">
              {browserSelectionMode === 'screenshot' ? 'Element screenshot' : 'Inspect details'}
            </span>
            {browserSelectionMode === 'screenshot' && browserSelectionCapture ? (
              <div className="chat-browser-selection-shot">
                <img src={browserSelectionCapture.dataUrl} alt={browserSelectionCapture.name} />
                <span className="chat-browser-selection-text">
                  {browserSelectionCapture.name}
                  {!useActiveBrowser ? ' (enable Browser to send)' : ''}
                </span>
              </div>
            ) : browserSelectionMode === 'screenshot' ? (
              <span className="chat-browser-selection-text">
                Preparing element screenshot...
                {!useActiveBrowser ? ' (enable Browser to send)' : ''}
              </span>
            ) : (
              <span className="chat-browser-selection-text">
                {describeBrowserSelection(browserSelection)}
                {!useActiveBrowser ? ' (enable Browser to send)' : ''}
              </span>
            )}
          </div>
          <button className="chat-browser-selection-clear" type="button" onClick={() => setBrowserSelection(null)}>
            x
          </button>
        </div>
      )}
      {mentionResults.length > 0 && mentionQuery !== null && (
        <div className="chat-commands-popup">
          {mentionResults.map((entry, i) => {
            const parts = entry.name.replace(/\\/g, '/');
            const lastSlash = parts.lastIndexOf('/');
            const fileName = lastSlash >= 0 ? parts.slice(lastSlash + 1) : parts;
            const dirPath = lastSlash >= 0 ? parts.slice(0, lastSlash) : '';
            return (
              <div
                key={entry.name}
                className={`chat-command-item ${i === mentionIdx ? 'active' : ''}${entry.ignored ? ' mention-ignored' : ''}`}
                onClick={() => selectMention(entry)}
              >
                <span className="chat-command-file-icon" aria-hidden="true">[]</span>
                <span className="chat-mention-filename">{fileName}</span>
                {dirPath && <span className="chat-mention-dir">{dirPath}</span>}
              </div>
            );
          })}
        </div>
      )}
      {filteredCommands.length > 0 && mentionQuery === null && (
        <div className="chat-commands-popup">
          {filteredCommands.map((cmd, i) => (
            <div
              key={cmd.name}
              className={`chat-command-item ${i === selectedIdx ? 'active' : ''}`}
              onClick={() => selectCommand(cmd.name)}
            >
              <span className="chat-command-name">/{cmd.name}</span>
            </div>
          ))}
        </div>
      )}
      {attachments.length > 0 && (
        <div className="chat-attachments">
          {attachments.map((att, i) => (
            <span key={i} className="chat-attachment-chip">
              {att.type === 'image' && (att.previewUrl || att.path) ? (
                <img className="chat-attachment-thumb" src={att.previewUrl ?? att.path} alt={att.name} />
              ) : (
                <span className="chat-attachment-icon" aria-hidden="true">[]</span>
              )}
              {att.name}
              <button className="chat-attachment-remove" onClick={() => removeAttachment(i)} type="button">x</button>
            </span>
          ))}
        </div>
      )}
      {voiceStatus && (
        <div className={`chat-voice-status${voiceActive ? ' active' : ''}`}>
          {voiceStatus}
        </div>
      )}
      <div className="chat-composer-shell">
        <div className="chat-textarea-wrap">
          <textarea
            ref={textareaRef}
            className="chat-textarea"
            placeholder="Ask something..."
            value={value}
            onChange={(e) => {
              setValue(e.target.value);
              setSelectedIdx(0);
              handleMentionDetect(e.target.value);
              adjustHeight();
            }}
            onPaste={handlePaste}
            onKeyDown={handleKeyDown}
            rows={1}
            disabled={disabled}
          />
        </div>
        <div className="chat-composer-actions">
          <button
            className="chat-attach-btn"
            onClick={() => fileInputRef.current?.click()}
            type="button"
            title="Attach file (or type @ to mention)"
            disabled={disabled}
          >
            <span aria-hidden="true">+</span>
          </button>
          {voiceSupported && (
            <button
              className={`chat-voice-btn${voiceActive ? ' active' : ''}`}
              onClick={toggleVoiceInput}
              type="button"
              title={voiceActive ? 'Stop voice input' : 'Start voice input'}
              disabled={disabled}
            >
              <MicIcon />
            </button>
          )}
          {streaming ? (
            <button className="chat-send-btn stop" onClick={onCancel} type="button" title="Stop response">
              <span aria-hidden="true">[]</span>
            </button>
          ) : (
            <button
              className="chat-send-btn"
              onClick={handleSend}
              disabled={(!value.trim() && attachments.length === 0) || disabled || !canSend}
              type="button"
              title="Send message"
            >
              <span aria-hidden="true">^</span>
            </button>
          )}
        </div>
      </div>
      <input
        ref={fileInputRef}
        type="file"
        style={{ display: 'none' }}
        accept="image/*,.txt,.md,.ts,.tsx,.js,.jsx,.go,.py,.rs,.json,.yaml,.toml,.css,.html"
        onChange={(e) => {
          const file = e.target.files?.[0];
          if (file) {
            const isImage = file.type.startsWith('image/');
            const previewUrl = isImage ? URL.createObjectURL(file) : undefined;
            setAttachments((prev) => [...prev, {
              type: isImage ? 'image' : 'file',
              path: previewUrl ?? file.name,
              previewUrl,
              name: file.name,
            }]);
          }
          e.target.value = '';
        }}
      />
    </div>
  );
}

function describeBrowserSelection(selection: BrowserElementSelection): string {
  const pieces = [selection.tagName.toLowerCase()];
  if (selection.role) {
    pieces.push(selection.role);
  }
  if (selection.text) {
    pieces.push(`"${selection.text}"`);
  }
  return pieces.join(' - ');
}
