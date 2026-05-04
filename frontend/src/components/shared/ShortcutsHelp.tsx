type ShortcutsHelpProps = {
  onClose: () => void;
};

const shortcuts = [
  { group: 'General', items: [
    { key: 'Ctrl+B', action: 'Toggle sidebar' },
    { key: 'Ctrl+`', action: 'Toggle terminal' },
    { key: 'Ctrl+Shift+G', action: 'Open git panel' },
    { key: 'Ctrl+Shift+L', action: 'Toggle chat panel' },
    { key: 'F1', action: 'Show keyboard shortcuts' },
  ]},
  { group: 'Editor', items: [
    { key: 'Ctrl+S', action: 'Save file' },
    { key: 'Ctrl+Shift+I', action: 'Inline AI prompt (select code first)' },
  ]},
  { group: 'Chat', items: [
    { key: 'Enter', action: 'Send message' },
    { key: 'Shift+Enter', action: 'New line' },
    { key: 'Escape', action: 'Close overlay / dismiss prompt' },
  ]},
];

export function ShortcutsHelp({ onClose }: ShortcutsHelpProps) {
  return (
    <div className="shortcuts-overlay" onClick={onClose}>
      <div className="shortcuts-modal" onClick={(e) => e.stopPropagation()}>
        <div className="shortcuts-title">Keyboard Shortcuts</div>
        {shortcuts.map((group) => (
          <div key={group.group} className="shortcuts-group">
            <div className="shortcuts-group-title">{group.group}</div>
            {group.items.map((item) => (
              <div key={item.key} className="shortcut-row">
                <span>{item.action}</span>
                <kbd className="shortcut-key">{item.key}</kbd>
              </div>
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}
