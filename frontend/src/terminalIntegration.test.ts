import { describe, expect, it } from 'vitest';

import {
  isShellLanguageCompatible,
  terminalCommandDialect,
  terminalShellLabel,
} from './terminalIntegration';

describe('terminalIntegration', () => {
  it('describes the active terminal shell dialect', () => {
    expect(terminalShellLabel('pwsh')).toBe('PowerShell');
    expect(terminalCommandDialect('pwsh')).toContain('PowerShell syntax');
    expect(terminalShellLabel('cmd')).toBe('Command Prompt');
    expect(terminalCommandDialect('wsl')).toBe('POSIX shell syntax');
  });

  it('matches runnable code blocks to the active shell', () => {
    expect(isShellLanguageCompatible('powershell', 'pwsh')).toBe(true);
    expect(isShellLanguageCompatible('bash', 'pwsh')).toBe(false);
    expect(isShellLanguageCompatible('cmd', 'cmd')).toBe(true);
    expect(isShellLanguageCompatible('bash', 'wsl')).toBe(true);
  });

});
