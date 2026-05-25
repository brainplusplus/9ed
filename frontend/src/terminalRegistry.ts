/**
 * Global registry for active terminal instances.
 *
 * TerminalView registers/unregisters itself here on mount/unmount.
 * Chat and other components read scrollback and send commands via this registry.
 */

export type TerminalHandle = {
  /** Read last N lines from the terminal scrollback buffer. */
  getScrollback: (maxLines: number) => string;
  /** Send a command string to the terminal (appends \r). Instant. */
  sendCommand: (command: string) => void;
  /** The working directory the terminal was started in. */
  cwd: string;
  /** Shell type (bash, zsh, powershell, etc). */
  shellType: string;
};

const registry = new Map<string, TerminalHandle>();

export function registerTerminal(id: string, handle: TerminalHandle): void {
  registry.set(id, handle);
}

export function unregisterTerminal(id: string): void {
  registry.delete(id);
}

export function getTerminalHandle(id: string): TerminalHandle | null {
  return registry.get(id) ?? null;
}

/** Get all registered terminal session IDs. */
export function getRegisteredTerminalIds(): string[] {
  return Array.from(registry.keys());
}

/**
 * Read scrollback from a terminal session.
 * Returns empty string if terminal not found.
 */
export function readTerminalScrollback(id: string, maxLines: number): string {
  const handle = registry.get(id);
  if (!handle) return '';
  return handle.getScrollback(maxLines);
}
