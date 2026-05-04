import { useEffect, useRef } from 'react';
import Editor, { type OnMount } from '@monaco-editor/react';
import type { editor } from 'monaco-editor';
import type { GutterChange } from '../../types';

type MonacoEditorProps = {
  value: string;
  language: string;
  onChange: (value: string) => void;
  onSave: () => void;
  gutterChanges?: GutterChange[];
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

export function MonacoEditor({ value, language, onChange, onSave, gutterChanges }: MonacoEditorProps) {
  const editorRef = useRef<editor.IStandaloneCodeEditor | null>(null);
  const decorationsRef = useRef<editor.IEditorDecorationsCollection | null>(null);

  const handleMount: OnMount = (editorInstance) => {
    editorRef.current = editorInstance;
    editorInstance.addCommand(
      // eslint-disable-next-line no-bitwise
      2048 | 49, // KeyMod.CtrlCmd | KeyCode.KeyS
      () => onSave(),
    );

    if (gutterChanges && gutterChanges.length > 0) {
      decorationsRef.current = editorInstance.createDecorationsCollection(gutterDecorations(gutterChanges));
    }
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
