import type { PendingPermission } from '../../types';

type PermissionDialogProps = {
  permission: PendingPermission;
  onRespond: (permissionId: string, optionId: string) => void;
  onReject: (permissionId: string) => void;
};

function kindToIcon(kind: string): string {
  switch (kind) {
    case 'allow_once': return '✓';
    case 'allow_always': return '✓✓';
    case 'reject_once': return '✕';
    case 'reject_always': return '✕✕';
    default: return '•';
  }
}

function kindToClass(kind: string): string {
  if (kind.startsWith('allow')) return 'perm-allow';
  if (kind.startsWith('reject')) return 'perm-reject';
  return '';
}

export function PermissionDialog({ permission, onRespond, onReject }: PermissionDialogProps) {
  const toolLabel = permission.toolKind
    ? `${permission.toolKind}${permission.title ? ': ' + permission.title : ''}`
    : permission.title || 'Tool execution';

  return (
    <div className="permission-dialog">
      <div className="permission-header">
        <span className="permission-icon">🔐</span>
        <span className="permission-label">Awaiting Confirmation</span>
      </div>
      <div className="permission-tool">{toolLabel}</div>
      <div className="permission-actions">
        {permission.options.map((opt) => (
          <button
            key={opt.optionId}
            className={`permission-btn ${kindToClass(opt.kind)}`}
            onClick={() => {
              if (opt.kind.startsWith('reject')) {
                onReject(permission.permissionId);
              } else {
                onRespond(permission.permissionId, opt.optionId);
              }
            }}
            type="button"
          >
            <span className="permission-btn-icon">{kindToIcon(opt.kind)}</span>
            {opt.name}
          </button>
        ))}
      </div>
    </div>
  );
}
