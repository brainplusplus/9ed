import { useEffect, useMemo, useState, type FormEvent } from 'react';

import {
  createSettingsTunnel,
  deleteSettingsTunnel,
  getSettingsAbout,
  getSettingsTunnels,
  restartSettingsTunnel,
  startSettingsTunnel,
  stopSettingsTunnel,
  updateSettingsTunnel,
} from '../../api';
import type { SettingsAboutInfo, SettingsTunnel } from '../../types';

type SettingsSection = 'tunneling' | 'about';

const POLL_INTERVAL_MS = 4000;

export function SettingsPanel() {
  const [section, setSection] = useState<SettingsSection>('tunneling');
  const [about, setAbout] = useState<SettingsAboutInfo | null>(null);
  const [tunnels, setTunnels] = useState<SettingsTunnel[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [name, setName] = useState('');
  const [localPort, setLocalPort] = useState('');
  const [formBusy, setFormBusy] = useState(false);
  const [actionBusyId, setActionBusyId] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    void getSettingsAbout()
      .then((info) => {
        if (!cancelled) setAbout(info);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load settings');
      });
    return () => { cancelled = true; };
  }, []);

  useEffect(() => {
    let cancelled = false;

    async function refresh(initial = false) {
      if (initial) setLoading(true);
      try {
        const next = await getSettingsTunnels();
        if (!cancelled) {
          setTunnels(next);
          setError(null);
        }
      } catch (err) {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : 'Failed to load tunnels');
        }
      } finally {
        if (!cancelled && initial) setLoading(false);
      }
    }

    void refresh(true);
    const timer = window.setInterval(() => {
      void refresh(false);
    }, POLL_INTERVAL_MS);

    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, []);

  const builtInTunnelNote = useMemo(() => {
    if (!about) return null;
    if (!about.appTunnelEnabled) return null;
    return `Port ${about.appPort} is reserved by 9ed's built-in tunnel while TUNNEL=true.`;
  }, [about]);

  function resetForm() {
    setEditingId(null);
    setName('');
    setLocalPort('');
    setFormOpen(false);
  }

  async function refreshTunnels() {
    const next = await getSettingsTunnels();
    setTunnels(next);
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setFormBusy(true);
    try {
      if (editingId) {
        await updateSettingsTunnel(editingId, {
          name,
          localPort,
          engine: about?.defaultTunnelEngine,
        });
      } else {
        await createSettingsTunnel({
          name,
          localPort,
          engine: about?.defaultTunnelEngine,
        });
      }
      await refreshTunnels();
      resetForm();
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save tunnel');
    } finally {
      setFormBusy(false);
    }
  }

  async function runAction(id: string, action: 'start' | 'stop' | 'restart' | 'delete') {
    setActionBusyId(id);
    try {
      if (action === 'start') await startSettingsTunnel(id);
      if (action === 'stop') await stopSettingsTunnel(id);
      if (action === 'restart') await restartSettingsTunnel(id);
      if (action === 'delete') await deleteSettingsTunnel(id);
      await refreshTunnels();
      if (editingId === id && action === 'delete') {
        resetForm();
      }
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : `Failed to ${action} tunnel`);
    } finally {
      setActionBusyId(null);
    }
  }

  function handleEdit(tunnel: SettingsTunnel) {
    setEditingId(tunnel.id);
    setName(tunnel.name);
    setLocalPort(tunnel.localPort);
    setFormOpen(true);
    setSection('tunneling');
  }

  return (
    <div className="settings-panel">
      <div className="settings-nav" role="tablist" aria-label="Settings sections">
        <button
          type="button"
          className={`settings-nav-btn${section === 'tunneling' ? ' active' : ''}`}
          onClick={() => setSection('tunneling')}
        >
          Tunneling
        </button>
        <button
          type="button"
          className={`settings-nav-btn${section === 'about' ? ' active' : ''}`}
          onClick={() => setSection('about')}
        >
          About
        </button>
      </div>

      {error && <div className="settings-banner error">{error}</div>}

      {section === 'tunneling' && (
        <div className="settings-scroll">
          <section className="settings-section">
            <div className="settings-section-header">
              <div>
                <h2>Tunnelings</h2>
                <p>Create public tunnels for other local ports on this machine.</p>
              </div>
              <button
                type="button"
                className="settings-primary-btn"
                onClick={() => {
                  if (formOpen && !editingId) {
                    resetForm();
                    return;
                  }
                  setFormOpen(true);
                  setEditingId(null);
                  setName('');
                  setLocalPort('');
                }}
              >
                {formOpen && !editingId ? 'Close' : 'New Tunnel'}
              </button>
            </div>

            {formOpen && (
              <form className="settings-card settings-form" onSubmit={handleSubmit}>
                <div className="settings-form-grid">
                  <label className="settings-field">
                    <span>Name</span>
                    <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Preview Site" required />
                  </label>
                  <label className="settings-field">
                    <span>Local Port</span>
                    <input value={localPort} onChange={(e) => setLocalPort(e.target.value)} inputMode="numeric" placeholder="3000" required />
                  </label>
                </div>
                <div className="settings-meta-row">
                  <span className="settings-engine-pill">Engine: {about?.defaultTunnelEngine ?? 'cloudflare'}</span>
                  <span className="settings-meta-copy">Engine is stored per tunnel, so later we can make this selectable without changing the data model.</span>
                </div>
                {builtInTunnelNote && <p className="settings-note">{builtInTunnelNote}</p>}
                <div className="settings-form-actions">
                  <button type="submit" className="settings-primary-btn" disabled={formBusy}>
                    {formBusy ? 'Saving...' : editingId ? 'Save Changes' : 'Create Tunnel'}
                  </button>
                  <button type="button" className="settings-secondary-btn" onClick={resetForm} disabled={formBusy}>
                    Cancel
                  </button>
                </div>
              </form>
            )}

            {loading ? <div className="settings-card settings-empty">Loading tunnels...</div> : null}
            {!loading && tunnels.length === 0 ? (
              <div className="settings-card settings-empty">
                No custom tunnels yet. Add one to expose another local service through {about?.defaultTunnelEngine ?? 'your default engine'}.
              </div>
            ) : null}

            <div className="settings-tunnel-list">
              {tunnels.map((tunnel) => {
                const isBusy = actionBusyId === tunnel.id;
                return (
                  <article key={tunnel.id} className="settings-card tunnel-card">
                    <div className="tunnel-card-top">
                      <div>
                        <div className="tunnel-card-title-row">
                          <h3>{tunnel.name}</h3>
                          <span className={`tunnel-status status-${tunnel.status}`}>{tunnel.status}</span>
                        </div>
                        <div className="tunnel-card-subtitle">
                          <span>Local port {tunnel.localPort}</span>
                          <span className="tunnel-engine-tag">{tunnel.engine}</span>
                        </div>
                      </div>
                    </div>

                    <div className="tunnel-url-block">
                      <span className="tunnel-label">Public URL</span>
                      {tunnel.url ? (
                        <a href={tunnel.url} target="_blank" rel="noreferrer" className="tunnel-url">
                          {tunnel.url}
                        </a>
                      ) : (
                        <span className="tunnel-url tunnel-url-muted">
                          {tunnel.status === 'starting' ? 'Waiting for public URL...' : 'Not running'}
                        </span>
                      )}
                    </div>

                    {tunnel.lastError && <p className="tunnel-error">{tunnel.lastError}</p>}

                    <div className="tunnel-actions">
                      {tunnel.status === 'started' ? (
                        <button type="button" className="settings-secondary-btn" onClick={() => void runAction(tunnel.id, 'stop')} disabled={isBusy}>
                          Stop
                        </button>
                      ) : (
                        <button type="button" className="settings-primary-btn" onClick={() => void runAction(tunnel.id, 'start')} disabled={isBusy}>
                          Start
                        </button>
                      )}
                      <button type="button" className="settings-secondary-btn" onClick={() => void runAction(tunnel.id, 'restart')} disabled={isBusy}>
                        Restart
                      </button>
                      <button type="button" className="settings-secondary-btn" onClick={() => handleEdit(tunnel)} disabled={isBusy}>
                        Edit
                      </button>
                      <button type="button" className="settings-danger-btn" onClick={() => void runAction(tunnel.id, 'delete')} disabled={isBusy}>
                        Delete
                      </button>
                    </div>
                  </article>
                );
              })}
            </div>
          </section>
        </div>
      )}

      {section === 'about' && (
        <div className="settings-scroll">
          <section className="settings-section">
            <article className="settings-card about-card">
              <div className="about-mark">9ed</div>
              <h2>{about?.name ?? '9ed'}</h2>
              <p>{about?.description ?? 'A browser-based IDE for terminals, code, git, agents, and browser workflows.'}</p>
              <div className="about-grid">
                <div>
                  <span className="about-label">Version</span>
                  <strong>{about?.version ?? 'dev'}</strong>
                </div>
                <div>
                  <span className="about-label">Default Tunnel Engine</span>
                  <strong>{about?.defaultTunnelEngine ?? 'cloudflare'}</strong>
                </div>
                <div>
                  <span className="about-label">9ed Port</span>
                  <strong>{about?.appPort ?? '-'}</strong>
                </div>
                <div>
                  <span className="about-label">Built-in Tunnel</span>
                  <strong>{about?.appTunnelEnabled ? 'Enabled' : 'Disabled'}</strong>
                </div>
              </div>
            </article>
          </section>
        </div>
      )}
    </div>
  );
}
