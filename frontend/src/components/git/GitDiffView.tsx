import { DiffEditor } from '@monaco-editor/react';

type GitDiffViewProps = {
  originalContent: string;
  modifiedContent: string;
  language: string;
  filePath: string;
};

export function GitDiffView({ originalContent, modifiedContent, language, filePath }: GitDiffViewProps) {
  return (
    <div className="monaco-wrapper">
      <DiffEditor
        height="100%"
        language={language}
        original={originalContent}
        modified={modifiedContent}
        theme="vs-dark"
        options={{
          readOnly: true,
          originalEditable: false,
          renderSideBySide: true,
          minimap: { enabled: false },
          fontSize: 14,
          fontFamily: "'IBM Plex Mono', Consolas, monospace",
          scrollBeyondLastLine: false,
          automaticLayout: true,
        }}
        modifiedModelPath={`modified://${filePath}`}
        originalModelPath={`original://${filePath}`}
      />
    </div>
  );
}
