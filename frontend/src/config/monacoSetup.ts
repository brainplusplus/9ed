import { loader } from '@monaco-editor/react';

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
