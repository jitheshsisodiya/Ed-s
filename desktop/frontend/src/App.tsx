import { useCallback, useEffect, useState, type FormEvent } from 'react';

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

import './App.css';

type Screen = 'login' | 'networks' | 'settings';

/** Unwraps the message from an error thrown across the Wails bridge. */
function errText(err: unknown): string {
  if (typeof err === 'string') return err;
  if (err instanceof Error) return err.message;
  return String(err);
}

function formatBytes(n: number): string {
  if (!n) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1);
  return `${(n / 1024 ** i).toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

export default function App() {
  const [screen, setScreen] = useState<Screen>('login');
  const [session, setSession] = useState<agent.Session | null>(null);
  const [version, setVersion] = useState('');
  const [status, setStatus] = useState<agent.Status | null>(null);
  const [logs, setLogs] = useState<string[]>([]);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const refreshSession = useCallback(async () => {
    const s = await GetSession();
    setSession(s);
    setScreen(s.loggedIn ? 'networks' : 'login');
  }, []);

  useEffect(() => {
    GetAppInfo()
      .then((info) => {
        setVersion(info.version);
        if (info.error) setError(info.error);
      })
      .catch((err) => setError(errText(err)));
    refreshSession().catch((err) => setError(errText(err)));
  }, [refreshSession]);

  // Engine progress arrives as events rather than being polled for.
  useEffect(() => {
    EventsOn('tunnel:log', (line: string) => {
      setLogs((prev) => [...prev.slice(-200), line]);
    });
    EventsOn('tunnel:disconnected', () => setStatus(null));
    return () => {
      EventsOff('tunnel:log');
      EventsOff('tunnel:disconnected');
    };
  }, []);

  // Poll live status so peer connection modes stay current.
  useEffect(() => {
    const tick = () => {
      GetStatus()
        .then((s) => setStatus(s.connected ? s : null))
        .catch(() => undefined);
    };
    tick();
    const id = window.setInterval(tick, 2000);
    return () => window.clearInterval(id);
  }, []);

  const run = useCallback(async (fn: () => Promise<void>) => {
    setBusy(true);
    setError('');
    try {
      await fn();
    } catch (err) {
      setError(errText(err));
    } finally {
      setBusy(false);
    }
  }, []);

  if (!session) {
    return (
      <div className="app center">
        <p className="muted">Starting NexusVPN…</p>
        {error && <p className="error">{error}</p>}
      </div>
    );
  }

  return (
    <div className="app">
      <header className="topbar">
        <div className="brand">
          NexusVPN <span className="muted small">{version}</span>
        </div>
        {session.loggedIn && (
          <nav className="nav">
            <button
              className={screen === 'networks' ? 'tab active' : 'tab'}
              onClick={() => setScreen('networks')}
            >
              Networks
            </button>
            <button
              className={screen === 'settings' ? 'tab active' : 'tab'}
              onClick={() => setScreen('settings')}
            >
              Settings
            </button>
            <button
              className="tab"
              onClick={() =>
                run(async () => {
                  await Logout();
                  await refreshSession();
                })
              }
            >
              Sign out
            </button>
          </nav>
        )}
      </header>

      {error && (
        <div className="error banner" role="alert">
          {error}
        </div>
      )}

      <main>
        {screen === 'login' && (
          <LoginScreen busy={busy} run={run} onDone={refreshSession} defaultServer={session.serverUrl} />
        )}
        {screen === 'settings' && (
          <SettingsScreen session={session} busy={busy} run={run} onChanged={refreshSession} />
        )}
        {screen === 'networks' && (
          <NetworksScreen status={status} logs={logs} busy={busy} run={run} onStatus={setStatus} />
        )}
      </main>
    </div>
  );
}

interface RunProps {
  busy: boolean;
  run: (fn: () => Promise<void>) => Promise<void>;
}

function LoginScreen({
  busy,
  run,
  onDone,
  defaultServer,
}: RunProps & { onDone: () => Promise<void>; defaultServer: string }) {
  const [server, setServer] = useState(defaultServer || 'https://');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [mfaCode, setMfaCode] = useState('');
  const [needsMfa, setNeedsMfa] = useState(false);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    void run(async () => {
      try {
        await Login(server, email, password, mfaCode);
      } catch (err) {
        // The engine names this case explicitly; show the code field rather
        // than surfacing it as a failure.
        if (errText(err).includes('mfa_required')) {
          setNeedsMfa(true);
          return;
        }
        throw err;
      }
      await onDone();
    });
  };

  return (
    <form className="card narrow" onSubmit={submit}>
      <h1>Sign in</h1>
      <label>
        Control plane
        <input
          value={server}
          onChange={(e) => setServer(e.target.value)}
          placeholder="https://api.example.com"
          required
        />
      </label>
      <label>
        Email
        <input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required />
      </label>
      <label>
        Password
        <input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required />
      </label>
      {needsMfa && (
        <label>
          Authentication code
          <input
            value={mfaCode}
            onChange={(e) => setMfaCode(e.target.value)}
            inputMode="numeric"
            autoFocus
            required
          />
        </label>
      )}
      <button className="primary" type="submit" disabled={busy}>
        {busy ? 'Signing in…' : 'Sign in'}
      </button>
    </form>
  );
}

function NetworksScreen({
  status,
  logs,
  busy,
  run,
  onStatus,
}: RunProps & {
  status: agent.Status | null;
  logs: string[];
  onStatus: (s: agent.Status | null) => void;
}) {
  const [networks, setNetworks] = useState<agent.Network[]>([]);
  const [inviteCode, setInviteCode] = useState('');
  const [newName, setNewName] = useState('');
  const [copied, setCopied] = useState('');

  const reload = useCallback(async () => {
    setNetworks(await ListNetworks());
  }, []);

  useEffect(() => {
    reload().catch(() => undefined);
  }, [reload]);

  // Copying an address is the action people reach for constantly: it goes
  // straight into a game browser, an RDP client or \\host file share.
  const copy = (text: string) => {
    void CopyToClipboard(text)
      .then(() => {
        setCopied(text);
        window.setTimeout(() => setCopied(''), 1500);
      })
      .catch(() => undefined);
  };

  return (
    <section className="card">
      {/* Your own address, front and centre: it is what you give people. */}
      <div className="row between selfbar">
        <div>
          <span className="muted small">Your address</span>
          <div className="selfip">
            {status ? (
              <button className="linky" onClick={() => copy(status.virtualIp)} title="Copy">
                {status.virtualIp}
              </button>
            ) : (
              <span className="muted">not connected</span>
            )}
          </div>
        </div>
        {status && (
          <div className="right muted small">
            {status.networkName || status.networkId}
            <br />
            {status.interfaceName} · NAT: {status.natType || 'unknown'}
          </div>
        )}
      </div>

      <h2>Networks</h2>
      {networks.length === 0 ? (
        <p className="muted">No networks yet. Create one, or join with an invite code.</p>
      ) : (
        <ul className="list">
          {networks.map((n) => {
            const active = status?.networkId === n.id;
            return (
              <li key={n.id} className={active ? 'active' : ''}>
                <div>
                  <strong>{n.name}</strong>
                  <p className="muted small">
                    {n.cidr} · {n.role} · {n.deviceCount} device{n.deviceCount === 1 ? '' : 's'}
                  </p>
                </div>
                {active ? (
                  <button
                    className="danger"
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
                    className="primary"
                    disabled={busy || status !== null}
                    title={status ? 'Disconnect first' : undefined}
                    onClick={() => run(() => Connect(n.id))}
                  >
                    Connect
                  </button>
                )}
              </li>
            );
          })}
        </ul>
      )}

      <div className="row gap">
        <form
          className="inline"
          onSubmit={(e) => {
            e.preventDefault();
            void run(async () => {
              await JoinNetwork(inviteCode);
              setInviteCode('');
              await reload();
            });
          }}
        >
          <input
            value={inviteCode}
            onChange={(e) => setInviteCode(e.target.value)}
            placeholder="Invite code"
            required
          />
          <button type="submit" disabled={busy}>
            Join
          </button>
        </form>

        <form
          className="inline"
          onSubmit={(e) => {
            e.preventDefault();
            void run(async () => {
              await CreateNetwork(newName, '', '');
              setNewName('');
              await reload();
            });
          }}
        >
          <input
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            placeholder="New network name"
            required
          />
          <button type="submit" disabled={busy}>
            Create
          </button>
        </form>
      </div>

      {status && (
        <>
          <h2>Computers</h2>
          {status.peers?.length ? (
            <table>
              <thead>
                <tr>
                  <th>Name</th>
                  <th>Address</th>
                  <th>Ping</th>
                  <th>Path</th>
                  <th>Sent</th>
                  <th>Received</th>
                </tr>
              </thead>
              <tbody>
                {status.peers.map((p) => (
                  <tr key={p.deviceId}>
                    <td>{p.deviceName || p.deviceId.slice(0, 8)}</td>
                    <td>
                      <button className="linky" onClick={() => copy(p.virtualIp)} title="Copy address">
                        {p.virtualIp}
                      </button>
                    </td>
                    <td>{p.latencyMs >= 0 ? `${p.latencyMs} ms` : '—'}</td>
                    <td>
                      <span className={`pill ${p.mode}`}>{p.mode}</span>
                    </td>
                    <td>{formatBytes(p.bytesSent)}</td>
                    <td>{formatBytes(p.bytesReceived)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          ) : (
            <p className="muted">No other computers on this network yet.</p>
          )}

          {copied && <p className="copied small">Copied {copied}</p>}

          <details className="activity">
            <summary className="muted small">Activity</summary>
            <pre className="log">{logs.slice(-40).join('\n') || 'No activity yet.'}</pre>
          </details>
        </>
      )}
    </section>
  );
}

function SettingsScreen({
  session,
  busy,
  run,
  onChanged,
}: RunProps & { session: agent.Session; onChanged: () => Promise<void> }) {
  const [deviceName, setDeviceName] = useState(session.deviceName);
  const [publicKey, setPublicKey] = useState(session.publicKey);

  return (
    <section className="card narrow">
      <h1>Settings</h1>
      <p className="muted">
        Signed in as {session.email} at {session.serverUrl}
      </p>

      <form
        onSubmit={(e) => {
          e.preventDefault();
          void run(async () => {
            await SetDeviceName(deviceName);
            await onChanged();
          });
        }}
      >
        <label>
          Device name
          <input value={deviceName} onChange={(e) => setDeviceName(e.target.value)} required />
        </label>
        <button type="submit" disabled={busy}>
          Save
        </button>
      </form>

      <h2>Device key</h2>
      <p className="muted small">
        The private key never leaves this device. Rotating it disconnects the tunnel and invalidates the
        old key network-wide once this device re-registers.
      </p>
      <code className="key">{publicKey}</code>
      <button
        className="danger"
        disabled={busy}
        onClick={() => run(async () => setPublicKey(await RotateDeviceKey()))}
      >
        Rotate key
      </button>
    </section>
  );
}
