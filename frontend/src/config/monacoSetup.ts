import { loader } from '@monaco-editor/react';

type ClipboardLike = {
  write?: (items: ClipboardItem[]) => Promise<void>;
  writeText?: (text: string) => Promise<void>;
};

installClipboardFallback();

const moduleResolutionErrors = [2307, 2304, 2305, 2306, 2552, 2686, 2792, 7016, 1259, 1192, 2694];
const implicitAnyErrors = [7006, 7031, 7034];
const typeCheckingWithoutFullTypes = [2339, 2345, 2322, 2769, 2554];
const jsxCompatErrors = [1005, 2875];
const jsOnlyErrors = [8010, 8002];

const suppressedCodes = [
  ...moduleResolutionErrors,
  ...implicitAnyErrors,
  ...typeCheckingWithoutFullTypes,
  ...jsxCompatErrors,
];

loader.init().then((monaco) => {
  const ts = monaco.languages.typescript;

  const compilerOptions = {
    target: ts.ScriptTarget.ESNext,
    module: ts.ModuleKind.ESNext,
    moduleResolution: ts.ModuleResolutionKind.NodeJs,
    jsx: ts.JsxEmit.ReactJSX,
    allowJs: true,
    checkJs: false,
    strict: false,
    noEmit: true,
    esModuleInterop: true,
    allowSyntheticDefaultImports: true,
    forceConsistentCasingInFileNames: false,
    resolveJsonModule: true,
    isolatedModules: true,
    skipLibCheck: true,
    allowNonTsExtensions: true,
    baseUrl: '.',
    paths: { '*': ['*'] },
  };

  ts.typescriptDefaults.setCompilerOptions(compilerOptions);
  ts.javascriptDefaults.setCompilerOptions(compilerOptions);

  ts.typescriptDefaults.setDiagnosticsOptions({
    noSemanticValidation: false,
    noSyntaxValidation: false,
    diagnosticCodesToIgnore: suppressedCodes,
  });

  ts.javascriptDefaults.setDiagnosticsOptions({
    noSemanticValidation: false,
    noSyntaxValidation: false,
    diagnosticCodesToIgnore: [...suppressedCodes, ...jsOnlyErrors],
  });

  ts.typescriptDefaults.setEagerModelSync(true);
  ts.javascriptDefaults.setEagerModelSync(true);
});

function installClipboardFallback() {
  if (typeof navigator === 'undefined') return;

  const existing = navigator.clipboard as ClipboardLike | undefined;
  const clipboard: ClipboardLike = {
    ...existing,
    writeText: async (text: string) => {
      try {
        if (existing?.writeText) {
          await existing.writeText.call(existing, text);
          return;
        }
      } catch {
      }
      await writeTextWithSelection(text);
    },
    write: async (items: ClipboardItem[]) => {
      try {
        if (existing?.write) {
          await existing.write.call(existing, items);
          return;
        }
      } catch {
      }
      await writeClipboardItems(items);
    },
  };

  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: clipboard,
  });
}

async function writeClipboardItems(items: ClipboardItem[]) {
  const text = await readPlainTextClipboardItem(items).catch(() => '');
  await writeTextWithSelection(text);
}

async function readPlainTextClipboardItem(items: ClipboardItem[]): Promise<string> {
  const item = items.find((candidate) => candidate.types.includes('text/plain'));
  if (!item) return '';

  const blob = await item.getType('text/plain');
  return blob.text();
}

async function writeTextWithSelection(text: string) {
  const textarea = document.createElement('textarea');
  textarea.value = text;
  textarea.setAttribute('readonly', '');
  textarea.style.position = 'fixed';
  textarea.style.opacity = '0';
  textarea.style.pointerEvents = 'none';
  document.body.appendChild(textarea);
  textarea.select();

  try {
    document.execCommand('copy');
  } finally {
    textarea.remove();
  }
}
