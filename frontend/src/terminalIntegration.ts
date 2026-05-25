export type ShellFamily = 'powershell' | 'cmd' | 'bash' | 'wsl' | 'zsh' | 'sh' | 'fish' | 'unknown';

export function normalizeTerminalShell(shellType: string | undefined): ShellFamily {
  const normalized = (shellType ?? '').toLowerCase();
  if (normalized.includes('powershell') || normalized === 'pwsh') return 'powershell';
  if (normalized === 'cmd' || normalized.includes('cmd.exe')) return 'cmd';
  if (normalized.includes('wsl')) return 'wsl';
  if (normalized.includes('bash')) return 'bash';
  if (normalized.includes('zsh')) return 'zsh';
  if (normalized.includes('fish')) return 'fish';
  if (normalized === 'sh') return 'sh';
  return 'unknown';
}

export function terminalShellLabel(shellType: string | undefined): string {
  switch (normalizeTerminalShell(shellType)) {
    case 'powershell':
      return 'PowerShell';
    case 'cmd':
      return 'Command Prompt';
    case 'wsl':
      return 'WSL/Linux shell';
    case 'bash':
      return 'bash';
    case 'zsh':
      return 'zsh';
    case 'sh':
      return 'POSIX sh';
    case 'fish':
      return 'fish';
    default:
      return shellType || 'unknown shell';
  }
}

export function terminalCommandDialect(shellType: string | undefined): string {
  switch (normalizeTerminalShell(shellType)) {
    case 'powershell':
      return 'PowerShell syntax and cmdlets, for example Get-ChildItem instead of ls -la';
    case 'cmd':
      return 'Windows Command Prompt syntax, for example dir instead of ls -la';
    case 'wsl':
    case 'bash':
    case 'zsh':
    case 'sh':
      return 'POSIX shell syntax';
    case 'fish':
      return 'fish shell syntax';
    default:
      return 'the active terminal shell syntax';
  }
}

export function isShellLanguageCompatible(lang: string | undefined, shellType: string | undefined): boolean {
  if (!lang) return false;
  const language = lang.toLowerCase();
  const shell = normalizeTerminalShell(shellType);

  if (shell === 'powershell') return language === 'powershell' || language === 'pwsh';
  if (shell === 'cmd') return language === 'cmd' || language === 'bat' || language === 'batch';
  if (shell === 'fish') return language === 'fish';
  if (shell === 'bash' || shell === 'wsl' || shell === 'zsh' || shell === 'sh') {
    return ['bash', 'sh', 'shell', 'zsh'].includes(language);
  }

  return ['bash', 'sh', 'shell', 'zsh', 'powershell', 'pwsh', 'cmd', 'bat', 'batch', 'fish'].includes(language);
}
