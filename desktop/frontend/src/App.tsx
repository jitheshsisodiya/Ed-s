import { useCallback, useEffect, useMemo, useState, type FormEvent, type ReactNode } from 'react';

import {
  Connect,
  CopyToClipboard,
  CreateNetwork,
  Disconnect,
  GetAppInfo,
  GetSession,
  GetStatus,
  JoinNetwork,
  ListNetworks,
  Login,
  Logout,
  RotateDeviceKey,
  SetDeviceName,
} from '../wailsjs/go/main/App';
import { EventsOff, EventsOn } from '../wailsjs/runtime/runtime';
import type { agent } from '../wailsjs/go/models';

import Icon, { deviceIcon } from './Icon';
import './App.css';

/* ============================================================
   Preferences kept in the webview, not the config file: they are
   presentation choices, and losing one costs a user nothing.
   ============================================================ */

type Theme = 'system' | 'light' | 'dark';

function usePreference(key: string, fallback: string) {
  const [value, setValue] = useState(() => localStorage.getItem(key) ?? fallback);
  const update = useCallback(
    (next: string) => {
      localStorage.setItem(key, next);
      setValue(next);
    },
    [key],
  );
  return [value, update] as const;
}

/** Turns an error from the Go bridge into something a person can read. */
function humanError(err: unknown): { message: string; fix?: string } {
  const raw = typeof err === 'string' ? err : err instanceof Error ? err.message : String(err);
  const lower = raw.toLowerCase();

  if (lower.includes('administrator') || lower.includes('root')) {
    return {
      message: 'NexusVPN needs permission to create a secure tunnel.',
      fix: 'Quit and reopen the app as an administrator. Nothing else on your computer changes.',
    };
  }
  if (lower.includes('not signed in') || lower.includes('unauthorized') || lower.includes('token')) {
    return { message: 'Your session has expired.', fix: 'Sign in again to continue.' };
  }
  if (lower.includes('invite')) {
    return { message: "That invite code didn't work.", fix: 'Codes expire — ask for a fresh one.' };
  }
  if (lower.includes('connection refused') || lower.includes('no such host') || lower.includes('dial')) {
    return {
      message: "Can't reach your server.",
      fix: 'Check that you are online and that the server address is right.',
    };
  }
  if (lower.includes('already connected')) {
    return { message: 'You are already connected.', fix: 'Disconnect first to switch networks.' };
  }
  return { message: raw };
}

function formatBytes(n: number): string {
  if (!n) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1);
  return `${(n / 1024 ** i).toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

/**
 * How traffic reaches a peer, said without jargon. The quality chip beside
 * it already carries the verdict, so this line answers "why" rather than
 * repeating "Online".
 */
function describePath(mode: string): string {
  switch (mode) {
    case 'direct':
      return 'Direct connection';
    case 'relay':
      return 'Connected through a relay';
    case 'connecting':
      return 'Finding the best route…';
    default:
      return 'Online';
  }
}

/** Human duration since an ISO timestamp — "just now", "4 min", "2 days". */
function since(iso: string): string {
  if (!iso) return '';
  const secs = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (secs < 45) return 'just now';
  if (secs < 3600) return `${Math.round(secs / 60)} min ago`;
  if (secs < 86400) return `${Math.round(secs / 3600)} hr ago`;
  return `${Math.round(secs / 86400)} days ago`;
}

/* ============================================================
   Shell
   ============================================================ */

export default function App() {
  const [session, setSession] = useState<agent.Session | null>(null);
  const [version, setVersion] = useState('');
  const [status, setStatus] = useState<agent.Status | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [error, setError] = useState<{ message: string; fix?: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [toast, setToast] = useState('');
  const [showSettings, setShowSettings] = useState(false);

  const [theme, setTheme] = usePreference('nexusvpn.theme', 'system');
  const [advanced, setAdvanced] = usePreference('nexusvpn.advanced', 'off');
  const isAdvanced = advanced === 'on';

  // Apply the theme choice to the document root; "system" removes the
  // override so prefers-color-scheme takes back over.
  useEffect(() => {
    const root = document.documentElement;
    if (theme === 'system') root.removeAttribute('data-theme');
    else root.setAttribute('data-theme', theme);
  }, [theme]);

  const refreshSession = useCallback(async () => {
    setSession(await GetSession());
  }, []);

  useEffect(() => {
    GetAppInfo()
      .then((info) => {
        setVersion(info.version);
        if (info.error) setError({ message: info.error });
      })
      .catch((err) => setError(humanError(err)));
    refreshSession().catch((err) => setError(humanError(err)));
  }, [refreshSession]);

  useEffect(() => {
    EventsOn('tunnel:log', (line: string) => setLogs((prev) => [...prev.slice(-200), line]));
    EventsOn('tunnel:disconnected', () => setStatus(null));
    return () => {
      EventsOff('tunnel:log');
      EventsOff('tunnel:disconnected');
    };
  }, []);

  useEffect(() => {
    const tick = () =>
      GetStatus()
        .then((s) => setStatus(s.connected ? s : null))
        .catch(() => undefined);
    tick();
    const id = window.setInterval(tick, 2000);
    return () => window.clearInterval(id);
  }, []);

  const notify = useCallback((text: string) => {
    setToast(text);
    window.setTimeout(() => setToast(''), 1800);
  }, []);

  const run = useCallback(async (fn: () => Promise<void>) => {
    setBusy(true);
    setError(null);
    try {
      await fn();
    } catch (err) {
      setError(humanError(err));
    } finally {
      setBusy(false);
    }
  }, []);

  const copy = useCallback(
    (text: string, label = 'Copied') => {
      void CopyToClipboard(text)
        .then(() => notify(label))
        .catch(() => undefined);
    },
    [notify],
  );

  const cycleTheme = () => {
    const next: Theme = theme === 'system' ? 'light' : theme === 'light' ? 'dark' : 'system';
    setTheme(next);
  };

  if (!session) {
    return (
      <div className="app center">
        <p className="substatus">Starting NexusVPN…</p>
      </div>
    );
  }

  return (
    <div className="app">
      <header className="titlebar">
        <div className="brand">
          <span className="mark" aria-hidden="true">
            N
          </span>
          NexusVPN
        </div>
        {session.loggedIn && (
          <div className="titlebar-actions">
            <button
              className="icon-btn"
              onClick={cycleTheme}
              title={`Appearance: ${theme}`}
              aria-label={`Appearance: ${theme}. Click to change.`}
            >
              <Icon name={theme === 'light' ? 'sun' : theme === 'dark' ? 'moon' : 'theme-auto'} />
            </button>
            <button
              className={showSettings ? 'icon-btn on' : 'icon-btn'}
              onClick={() => setShowSettings((v) => !v)}
              title="Settings"
              aria-label="Settings"
            >
              <Icon name="settings" />
            </button>
          </div>
        )}
      </header>

      <main className={session.loggedIn ? undefined : 'entry'}>
        <div className="sheet">
          {error && (
            <div className="banner fade-in" role="alert">
              <span className="glyph">
                <Icon name="alert" size={17} />
              </span>
              <div>
                <p>{error.message}</p>
                {error.fix && <p className="fix">{error.fix}</p>}
              </div>
            </div>
          )}

          {!session.loggedIn ? (
            <Welcome busy={busy} run={run} onDone={refreshSession} defaultServer={session.serverUrl} />
          ) : showSettings ? (
            <Settings
              session={session}
              version={version}
              busy={busy}
              run={run}
              advanced={isAdvanced}
              onAdvanced={(on) => setAdvanced(on ? 'on' : 'off')}
              theme={theme as Theme}
              onTheme={setTheme}
              onChanged={refreshSession}
              onClose={() => setShowSettings(false)}
              onCopy={copy}
            />
          ) : (
            <Home
              status={status}
              logs={logs}
              busy={busy}
              run={run}
              advanced={isAdvanced}
              onStatus={setStatus}
              onCopy={copy}
            />
          )}
        </div>
      </main>

      {toast && <div className="toast">{toast}</div>}
    </div>
  );
}

interface Common {
  busy: boolean;
  run: (fn: () => Promise<void>) => Promise<void>;
}

/* ============================================================
   Welcome — one screen, one decision
   ============================================================ */

function Welcome({
  busy,
  run,
  onDone,
  defaultServer,
}: Common & { onDone: () => Promise<void>; defaultServer: string }) {
  const [showServer, setShowServer] = useState(false);
  const [server, setServer] = useState(defaultServer);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [mfaCode, setMfaCode] = useState('');
  const [needsMfa, setNeedsMfa] = useState(false);
  const [withEmail, setWithEmail] = useState(false);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    void run(async () => {
      try {
        await Login(server, email, password, mfaCode);
      } catch (err) {
        const raw = typeof err === 'string' ? err : String(err);
        if (raw.includes('mfa_required')) {
          setNeedsMfa(true);
          return;
        }
        throw err;
      }
      await onDone();
    });
  };

  return (
    <>
      <div className="welcome">
        <div className="mark-lg" aria-hidden="true">
          N
        </div>
        <h1>Welcome to NexusVPN</h1>
        <p>Connect your computers as if they were in the same room.</p>
      </div>

      {!withEmail ? (
        <div className="card">
          <div className="providers">
            <button className="provider" onClick={() => setWithEmail(true)}>
              <span className="glyph">
                <Icon name="mail" size={17} />
              </span>
              Continue with email
            </button>
            {/* Only Google is wired in the backend today. The others are
                shown so the path is obvious, and disabled so nobody hits a
                dead end. */}
            <button className="provider" disabled>
              <span className="glyph" aria-hidden="true">
                G
              </span>
              Continue with Google
              <span className="soon">Soon</span>
            </button>
            <button className="provider" disabled>
              <span className="glyph" aria-hidden="true">
                ⊞
              </span>
              Continue with Microsoft
              <span className="soon">Soon</span>
            </button>
          </div>
        </div>
      ) : (
        <form className="card fade-in" onSubmit={submit} style={{ display: 'grid', gap: 14 }}>
          <div className="field">
            <label htmlFor="email">Email</label>
            <input
              id="email"
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              autoFocus
              required
            />
          </div>
          <div className="field">
            <label htmlFor="password">Password</label>
            <input
              id="password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
          </div>

          {needsMfa && (
            <div className="field fade-in">
              <label htmlFor="mfa">Authentication code</label>
              <input
                id="mfa"
                value={mfaCode}
                onChange={(e) => setMfaCode(e.target.value)}
                inputMode="numeric"
                placeholder="123456"
                autoFocus
                required
              />
              <span className="hint">From your authenticator app.</span>
            </div>
          )}

          {showServer ? (
            <div className="field fade-in">
              <label htmlFor="server">Your server</label>
              <input
                id="server"
                value={server}
                onChange={(e) => setServer(e.target.value)}
                placeholder="https://nexus.example.com"
                required
              />
              <span className="hint">Leave this alone unless you run your own.</span>
            </div>
          ) : (
            <button type="button" className="text-btn" onClick={() => setShowServer(true)}>
              Use your own server
            </button>
          )}

          <button className="btn primary block" type="submit" disabled={busy}>
            {busy ? 'Signing in…' : 'Sign in'}
          </button>
          <button type="button" className="text-btn" onClick={() => setWithEmail(false)}>
            Back
          </button>
        </form>
      )}
    </>
  );
}

/* ============================================================
   Home — status, networks, devices
   ============================================================ */

function Home({
  status,
  logs,
  busy,
  run,
  advanced,
  onStatus,
  onCopy,
}: Common & {
  status: agent.Status | null;
  logs: string[];
  advanced: boolean;
  onStatus: (s: agent.Status | null) => void;
  onCopy: (text: string, label?: string) => void;
}) {
  const [networks, setNetworks] = useState<agent.Network[]>([]);
  const [loaded, setLoaded] = useState(false);
  // The network this machine last joined. Without it the big control has no
  // target once someone belongs to more than one network, and a button that
  // does nothing when clicked is worse than no button.
  const [lastNetwork, setLastNetwork] = usePreference('nexusvpn.lastNetwork', '');
  const [inviteCode, setInviteCode] = useState('');
  const [newName, setNewName] = useState('');
  const [adding, setAdding] = useState(false);

  const reload = useCallback(async () => {
    setNetworks(await ListNetworks());
    setLoaded(true);
  }, []);

  useEffect(() => {
    reload().catch(() => setLoaded(true));
  }, [reload]);

  const connected = status !== null;
  const online = useMemo(
    () => (status?.peers ?? []).filter((p) => p.quality !== 'Offline').length,
    [status],
  );
  const active = networks.find((n) => n.id === status?.networkId);
  // One network needs no choosing; otherwise fall back to the last one used.
  const target =
    networks.length === 1 ? networks[0] : networks.find((n) => n.id === lastNetwork);

  const connectTo = useCallback(
    (id: string) =>
      run(async () => {
        setLastNetwork(id);
        await Connect(id);
      }),
    [run, setLastNetwork],
  );

  const toggle = () => {
    if (connected) {
      void run(async () => {
        await Disconnect();
        onStatus(null);
      });
      return;
    }
    if (target) void connectTo(target.id);
  };

  const statusWord = connected ? 'Connected' : busy ? 'Connecting' : 'Not connected';
  const orbClass = connected ? 'orb connected' : busy ? 'orb busy' : 'orb';

  return (
    <>
      <section className="hero">
        <button
          className={orbClass}
          onClick={toggle}
          disabled={busy || (!connected && !target)}
          title={!connected && !target ? 'Choose a network below' : undefined}
        >
          {connected ? 'Disconnect' : busy ? 'Connecting…' : 'Connect'}
        </button>

        <div className="status-line">
          <span className={connected ? 'led good' : busy ? 'led warn' : 'led bad'} />
          {connected ? active?.name || status.networkName || 'Connected' : statusWord}
        </div>

        <p className="substatus">
          {connected
            ? `Secure · ${online} of ${status.peers?.length ?? 0} devices online`
            : networks.length === 0
              ? 'Create a network or join one to get started'
              : target
                ? `Connect to ${target.name}`
                : 'Choose a network below'}
        </p>
      </section>

      {/* Networks: hidden entirely when there is exactly one and it's live,
          because at that point the list is noise. */}
      {(!connected || networks.length > 1) && (
        <section className="card">
          <div className="section-head">
            <h2>Networks</h2>
            <button className="text-btn" onClick={() => setAdding((v) => !v)}>
              {adding ? 'Done' : 'Add'}
            </button>
          </div>

          {!loaded ? (
            <p className="substatus">Loading…</p>
          ) : networks.length === 0 && !adding ? (
            <div className="state">
              <div className="glyph">
                <Icon name="network" size={26} />
              </div>
              <h3>No networks yet</h3>
              <p>Create one for your own devices, or join a friend&rsquo;s with their invite code.</p>
              <button className="btn primary" onClick={() => setAdding(true)}>
                Get started
              </button>
            </div>
          ) : (
            <div className="rows">
              {networks.map((n) => {
                const isActive = status?.networkId === n.id;
                return (
                  <div key={n.id} className={isActive ? 'row active' : 'row'}>
                    <div className="row-icon">
                      <Icon name="network" />
                    </div>
                    <div className="row-main">
                      <div className="row-title">
                        {n.name}
                        {isActive && <span className="badge">Live</span>}
                      </div>
                      <div className="row-sub">
                        {n.deviceCount} device{n.deviceCount === 1 ? '' : 's'}
                        {advanced && ` · ${n.cidr} · ${n.role}`}
                      </div>
                    </div>
                    {isActive ? (
                      <button
                        className="btn danger"
                        disabled={busy}
                        onClick={() =>
                          run(async () => {
                            await Disconnect();
                            onStatus(null);
                          })
                        }
                      >
                        Disconnect
                      </button>
                    ) : (
                      <button
                        className="btn"
                        disabled={busy || connected}
                        title={connected ? 'Disconnect first' : undefined}
                        onClick={() => connectTo(n.id)}
                      >
                        Connect
                      </button>
                    )}
                  </div>
                );
              })}
            </div>
          )}

          {adding && (
            <div className="fade-in" style={{ display: 'grid', gap: 12, marginTop: 14 }}>
              <form
                className="field"
                onSubmit={(e) => {
                  e.preventDefault();
                  void run(async () => {
                    await JoinNetwork(inviteCode);
                    setInviteCode('');
                    setAdding(false);
                    await reload();
                  });
                }}
              >
                <label htmlFor="invite">Have an invite code?</label>
                <div className="inline-form">
                  <input
                    id="invite"
                    value={inviteCode}
                    onChange={(e) => setInviteCode(e.target.value)}
                    placeholder="K7M2QP"
                    required
                  />
                  <button className="btn primary" type="submit" disabled={busy}>
                    Join
                  </button>
                </div>
              </form>

              <div className="divider">or</div>

              <form
                className="field"
                onSubmit={(e) => {
                  e.preventDefault();
                  void run(async () => {
                    await CreateNetwork(newName, '', '');
                    setNewName('');
                    setAdding(false);
                    await reload();
                  });
                }}
              >
                <label htmlFor="netname">Start a new network</label>
                <div className="inline-form">
                  <input
                    id="netname"
                    value={newName}
                    onChange={(e) => setNewName(e.target.value)}
                    placeholder="Home Lab"
                    required
                  />
                  <button className="btn" type="submit" disabled={busy}>
                    Create
                  </button>
                </div>
                {/* No CIDR, no DNS, no routing: the server picks sane
                    defaults and Advanced can reveal them later. */}
                <span className="hint">We&rsquo;ll handle the networking details.</span>
              </form>
            </div>
          )}
        </section>
      )}

      {connected && (
        <section className="card fade-in">
          <div className="section-head">
            <h2>Devices</h2>
            <button className="text-btn" onClick={() => onCopy(status.virtualIp, 'Your address copied')}>
              Copy my address
            </button>
          </div>

          {status.peers?.length ? (
            <div className="rows">
              {status.peers.map((p) => (
                <DeviceRow key={p.deviceId} peer={p} advanced={advanced} onCopy={onCopy} />
              ))}
            </div>
          ) : (
            <div className="state">
              <div className="glyph">
                <Icon name="devices" size={26} />
              </div>
              <h3>No other devices yet</h3>
              <p>
                Install NexusVPN on another computer and sign in with the same account — it will appear
                here.
              </p>
            </div>
          )}

          {advanced && (
            <div className="advanced">
              <dl className="kv">
                <dt>Your address</dt>
                <dd>{status.virtualIp}</dd>
                <dt>Network range</dt>
                <dd>{status.cidr}</dd>
                <dt>Interface</dt>
                <dd>{status.interfaceName}</dd>
                <dt>NAT type</dt>
                <dd>{status.natType || 'unknown'}</dd>
                <dt>Public endpoint</dt>
                <dd>{status.publicEndpoint || '—'}</dd>
              </dl>
              <div className="section-head" style={{ marginTop: 16 }}>
                <h2>Activity</h2>
              </div>
              <pre className="log">{logs.slice(-40).join('\n') || 'Nothing yet.'}</pre>
            </div>
          )}
        </section>
      )}
    </>
  );
}

/** One device. Addressed by name; the address itself only in Advanced. */
function DeviceRow({
  peer,
  advanced,
  onCopy,
}: {
  peer: agent.Peer;
  advanced: boolean;
  onCopy: (text: string, label?: string) => void;
}) {
  const quality = peer.quality || 'Offline';
  const offline = quality === 'Offline';

  return (
    <div className={offline ? 'row dim' : 'row'}>
      <div className="row-icon">
        <Icon name={deviceIcon(peer.os)} />
      </div>
      <div className="row-main">
        <div className="row-title">{peer.deviceName || 'Unnamed device'}</div>
        <div className="row-sub">
          {offline
            ? peer.lastHandshake
              ? `Last seen ${since(peer.lastHandshake)}`
              : 'Not connected yet'
            : advanced
              ? `${peer.virtualIp} · ${peer.mode} · ${formatBytes(peer.bytesSent)} sent`
              : describePath(peer.mode)}
        </div>
      </div>
      <span className={`quality ${quality.toLowerCase()}`}>{quality}</span>
      <button
        className="icon-btn"
        onClick={() => onCopy(peer.virtualIp, `${peer.deviceName || 'Device'} address copied`)}
        title="Copy address"
        aria-label={`Copy the address for ${peer.deviceName || 'this device'}`}
      >
        <Icon name="copy" size={16} />
      </button>
    </div>
  );
}

/* ============================================================
   Settings
   ============================================================ */

function Settings({
  session,
  version,
  busy,
  run,
  advanced,
  onAdvanced,
  theme,
  onTheme,
  onChanged,
  onClose,
  onCopy,
}: Common & {
  session: agent.Session;
  version: string;
  advanced: boolean;
  onAdvanced: (on: boolean) => void;
  theme: Theme;
  onTheme: (t: Theme) => void;
  onChanged: () => Promise<void>;
  onClose: () => void;
  onCopy: (text: string, label?: string) => void;
}) {
  const [deviceName, setDeviceName] = useState(session.deviceName);
  const [publicKey, setPublicKey] = useState(session.publicKey);

  return (
    <>
      <div className="section-head">
        <h2>Settings</h2>
        <button className="text-btn" onClick={onClose}>
          Done
        </button>
      </div>

      <section className="card" style={{ display: 'grid', gap: 16 }}>
        <form
          className="field"
          onSubmit={(e) => {
            e.preventDefault();
            void run(async () => {
              await SetDeviceName(deviceName);
              await onChanged();
            });
          }}
        >
          <label htmlFor="devicename">This device is called</label>
          <div className="inline-form">
            <input
              id="devicename"
              value={deviceName}
              onChange={(e) => setDeviceName(e.target.value)}
              required
            />
            <button className="btn" type="submit" disabled={busy}>
              Save
            </button>
          </div>
          <span className="hint">This is the name other people see.</span>
        </form>

        <Row label="Appearance">
          <div style={{ display: 'flex', gap: 6 }}>
            {(['system', 'light', 'dark'] as Theme[]).map((t) => (
              <button
                key={t}
                className={theme === t ? 'btn primary' : 'btn'}
                style={{ padding: '6px 12px', fontSize: 12.5 }}
                onClick={() => onTheme(t)}
              >
                {t === 'system' ? 'Auto' : t === 'light' ? 'Light' : 'Dark'}
              </button>
            ))}
          </div>
        </Row>

        <button className="switch" onClick={() => onAdvanced(!advanced)} aria-pressed={advanced}>
          <span>
            <span style={{ fontWeight: 560 }}>Advanced mode</span>
            <span className="hint" style={{ display: 'block' }}>
              Show addresses, routes and connection details.
            </span>
          </span>
          <span className={advanced ? 'track on' : 'track'}>
            <span className="knob" />
          </span>
        </button>
      </section>

      <section className="card">
        <div className="section-head">
          <h2>Account</h2>
        </div>
        <div className="rows">
          <div className="row">
            <div className="row-icon">
              <Icon name="user" />
            </div>
            <div className="row-main">
              <div className="row-title">{session.email}</div>
              <div className="row-sub">{session.serverUrl}</div>
            </div>
            <button className="btn danger" disabled={busy} onClick={() => run(() => Logout())}>
              Sign out
            </button>
          </div>
        </div>
      </section>

      {advanced && (
        <section className="card fade-in">
          <div className="section-head">
            <h2>Device key</h2>
          </div>
          <p className="hint" style={{ marginTop: 0 }}>
            The private half never leaves this device. Rotating disconnects the tunnel and retires the old
            key everywhere once this device signs back in.
          </p>
          <dl className="kv" style={{ margin: '12px 0' }}>
            <dt>Public key</dt>
            <dd>{publicKey}</dd>
          </dl>
          <div style={{ display: 'flex', gap: 8 }}>
            <button className="btn" onClick={() => onCopy(publicKey, 'Public key copied')}>
              Copy
            </button>
            <button
              className="btn danger"
              disabled={busy}
              onClick={() => run(async () => setPublicKey(await RotateDeviceKey()))}
            >
              Rotate key
            </button>
          </div>
        </section>
      )}

      <p className="substatus">NexusVPN {version}</p>
    </>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 14 }}>
      <span style={{ fontWeight: 560 }}>{label}</span>
      {children}
    </div>
  );
}
