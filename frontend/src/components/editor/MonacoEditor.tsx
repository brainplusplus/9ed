import { useCallback, useEffect, useRef } from 'react';
import Editor, { type OnMount } from '@monaco-editor/react';
import type { editor } from 'monaco-editor';
import type { CodeContext, GutterChange } from '../../types';

type SelectionInfo = {
  context: CodeContext;
  position: { top: number; left: number };
};

type MonacoEditorProps = {
  value: string;
  language: string;
  filePath?: string;
  onChange: (value: string) => void;
  onSave: () => void;
  gutterChanges?: GutterChange[];
  onSelectionChange?: (selection: SelectionInfo | null) => void;
};

function gutterDecorations(changes: GutterChange[]): editor.IModelDeltaDecoration[] {
  return changes.map((change) => ({
    range: {
      startLineNumber: change.startLine,
      startColumn: 1,
      endLineNumber: change.endLine,
      endColumn: 1,
    },
    options: {
      isWholeLine: true,
      linesDecorationsClassName: `git-gutter-${change.type}`,
    },
  }));
}

export function MonacoEditor({ value, language, filePath, onChange, onSave, gutterChanges, onSelectionChange }: MonacoEditorProps) {
  const editorRef = useRef<editor.IStandaloneCodeEditor | null>(null);
  const decorationsRef = useRef<editor.IEditorDecorationsCollection | null>(null);
  const selectionTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const onSaveRef = useRef(onSave);
  onSaveRef.current = onSave;

  const checkSelection = useCallback(() => {
    const ed = editorRef.current;
    if (!ed || !onSelectionChange) return;

    const selection = ed.getSelection();
    if (!selection || selection.isEmpty()) {
      onSelectionChange(null);
      return;
    }

    const model = ed.getModel();
    if (!model) return;

    const selectedText = model.getValueInRange(selection);
    if (selectedText.length <= 10) {
      onSelectionChange(null);
      return;
    }

    const endPos = selection.getEndPosition();
    const coords = ed.getScrolledVisiblePosition(endPos);
    const domNode = ed.getDomNode();
    if (!coords || !domNode) {
      onSelectionChange(null);
      return;
    }

    const rect = domNode.getBoundingClientRect();
    onSelectionChange({
      context: {
        filePath: filePath ?? '',
        startLine: selection.startLineNumber,
        endLine: selection.endLineNumber,
        selectedCode: selectedText,
        language,
      },
      position: {
        top: rect.top + coords.top + coords.height + 4,
        left: rect.left + coords.left,
      },
    });
  }, [onSelectionChange, filePath, language]);

  const handleMount: OnMount = (editorInstance) => {
    editorRef.current = editorInstance;
    editorInstance.addCommand(
      // eslint-disable-next-line no-bitwise
      2048 | 49,
      () => onSaveRef.current(),
    );

    if (gutterChanges && gutterChanges.length > 0) {
      decorationsRef.current = editorInstance.createDecorationsCollection(gutterDecorations(gutterChanges));
    }

    editorInstance.onDidChangeCursorSelection(() => {
      if (selectionTimerRef.current) clearTimeout(selectionTimerRef.current);
      selectionTimerRef.current = setTimeout(checkSelection, 500);
    });

    editorInstance.onDidBlurEditorWidget(() => {
      if (onSelectionChange) onSelectionChange(null);
    });
  };

  useEffect(() => {
    if (!editorRef.current || !gutterChanges) return;
    if (decorationsRef.current) {
      decorationsRef.current.set(gutterDecorations(gutterChanges));
    } else {
      decorationsRef.current = editorRef.current.createDecorationsCollection(gutterDecorations(gutterChanges));
    }
  }, [gutterChanges]);

  useEffect(() => {
    return () => {
      if (selectionTimerRef.current) clearTimeout(selectionTimerRef.current);
      decorationsRef.current = null;
      editorRef.current = null;
    };
  }, []);

  return (
    <div className="monaco-wrapper">
      <Editor
        height="100%"
        language={language}
        value={value}
        path={filePath}
        theme="vs-dark"
        onChange={(val) => onChange(val ?? '')}
        onMount={handleMount}
        options={{
          minimap: { enabled: true },
          fontSize: 14,
          fontFamily: "'IBM Plex Mono', Consolas, monospace",
          wordWrap: 'on',
          scrollBeyondLastLine: false,
          automaticLayout: true,
        }}
      />
    </div>
  );
}
