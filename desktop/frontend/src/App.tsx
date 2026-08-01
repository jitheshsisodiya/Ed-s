import { useCallback, useEffect, useRef, useState, type FormEvent } from 'react';
import { AnimatePresence, motion } from 'framer-motion';
import {
  AlertTriangle,
  Bell,
  KeyRound,
  Link2,
  Server,
  ShieldAlert,
  Globe,
  Sliders,
  X,
} from 'lucide-react';

import {
  Connect,
  CopyToClipboard,
  CreateNetwork,
  Disconnect,
  GetAppInfo,
  GetLocalServer,
  GetSession,
  GetStartWithSystem,
  GetStatus,
  JoinNetwork,
  ListNetworks,
  Login,
  Logout,
  Register,
  RotateDeviceKey,
  SetReachableFromAnywhere,
  SetRelayHere,
  SetServerRole,
  SetDeviceName,
  SetStartWithSystem,
  StopUsingExitNode,
  TakePendingInvite,
  UseExitNode,
} from '../wailsjs/go/main/App';
import { EventsOff, EventsOn } from '../wailsjs/runtime/runtime';
import { Quit } from '../wailsjs/runtime/runtime';
import type { agent, main } from '../wailsjs/go/models';

import Dialog, { Danger, Input, Primary, Row, Secondary, Toggle } from './Dialog';
import Invite from './Invite';
import PairPhone from './PairPhone';
import MenuBar, { item, separator } from './MenuBar';
import NetworkTree, { setSelfAddress } from './NetworkTree';
import ReactorCore, { type Phase } from './ReactorCore';
import Scrambler from './Scrambler';
import Telemetry, { type Sample } from './Telemetry';
import './theme.css';

/* ============================================================
   Shell.

   Radmin's layout: a menu bar, an identity strip carrying the
   one switch and your own address, then the tree of networks and
   machines filling everything below it. The instrumentation lives
   in a status strip along the bottom, where it can be read but
   never competes with the list.
   ============================================================ */

type Modal =
  | { kind: 'create' }
  | { kind: 'join'; code?: string }
  | { kind: 'settings' }
  | { kind: 'rename' }
  | { kind: 'properties'; peer: agent.Peer }
  | { kind: 'invite'; network: agent.Network }
  | { kind: 'pair'; network: agent.Network }
  | null;

export default function App() {
  const [session, setSession] = useState<agent.Session | null>(null);
  const [version, setVersion] = useState('');
  const [status, setStatus] = useState<agent.Status | null>(null);
  const [networks, setNetworks] = useState<agent.Network[]>([]);
  const [logs, setLogs] = useState<string[]>([]);
  const [error, setError] = useState<{ message: string; fix?: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [toast, setToast] = useState('');
  const [modal, setModal] = useState<Modal>(null);
  const [history, setHistory] = useState<Sample[]>([]);
  const previous = useRef<{ sent: number; recv: number } | null>(null);

  const [server, setServer] = useState<main.LocalServer | null>(null);

  const [lastNetwork, setLastNetwork] = usePreference('nexusvpn.lastNetwork', '');
  const [killSwitchPref, setKillSwitchPref] = usePreference('nexusvpn.killSwitch', 'on');
  const [allowLanPref, setAllowLanPref] = usePreference('nexusvpn.allowLan', 'on');

  const refreshSession = useCallback(async () => setSession(await GetSession()), []);
  const refreshNetworks = useCallback(async () => setNetworks(await ListNetworks()), []);

  useEffect(() => {
    GetAppInfo()
      .then((info) => {
        setVersion(info.version);
        if (info.error) setError({ message: info.error });
      })
      .catch((err) => setError(humanError(err)));
    GetLocalServer().then(setServer).catch(() => undefined);
    refreshSession().catch((err) => setError(humanError(err)));
  }, [refreshSession]);

  useEffect(() => {
    if (session?.loggedIn) refreshNetworks().catch(() => undefined);
  }, [session?.loggedIn, refreshNetworks]);

  useEffect(() => {
    EventsOn('tunnel:log', (line: string) => setLogs((p) => [...p.slice(-300), line]));
    EventsOn('tunnel:disconnected', () => setStatus(null));
    // An invite link opens the join dialog with the code filled in — it never
    // joins on its own. A link somebody sent is a suggestion, and you should
    // see which network it is before you are on it.
    EventsOn('invite:offered', (code: string) => setModal({ kind: 'join', code }));
    return () => {
      EventsOff('tunnel:log');
      EventsOff('tunnel:disconnected');
      EventsOff('invite:offered');
    };
  }, []);

  // A link that launched the app fires its event before this window exists,
  // so the code waits on the Go side until something asks for it.
  useEffect(() => {
    TakePendingInvite()
      .then((code) => code && setModal({ kind: 'join', code }))
      .catch(() => undefined);
  }, []);

  // One poll feeds the readouts and the traces, so a figure in the list and
  // a point on a graph always come from the same instant.
  useEffect(() => {
    const tick = () =>
      GetStatus()
        .then((s) => {
          setStatus(s.connected ? s : null);
          setSelfAddress(s.connected ? s.virtualIp : '');
          if (!s.connected) {
            previous.current = null;
            return;
          }
          const sent = s.peers?.reduce((a, p) => a + p.bytesSent, 0) ?? 0;
          const recv = s.peers?.reduce((a, p) => a + p.bytesReceived, 0) ?? 0;
          const prev = previous.current;
          previous.current = { sent, recv };
          // The first tick has nothing to subtract from, so charting it
          // would render the whole session as one spike and flatten
          // everything that came after.
          if (!prev) return;
          setHistory((h) =>
            [
              ...h,
              {
                latencyMs: bestLatency(s.peers ?? []),
                sentDelta: Math.max(0, (sent - prev.sent) / 2),
                recvDelta: Math.max(0, (recv - prev.recv) / 2),
              },
            ].slice(-60),
          );
        })
        .catch(() => undefined);
    tick();
    const id = window.setInterval(tick, 2000);
    return () => window.clearInterval(id);
  }, []);

  const notify = useCallback((text: string) => {
    setToast(text);
    window.setTimeout(() => setToast(''), 1900);
  }, []);

  /// Drops everything the signed-in screen was showing. Networks and peers
  /// belong to an account; leaving them on screen after that account is gone
  /// shows a list nothing can act on.
  const forgetSession = useCallback(async () => {
    setStatus(null);
    setNetworks([]);
    setHistory([]);
    setModal(null);
    previous.current = null;
    await refreshSession().catch(() => undefined);
  }, [refreshSession]);

  const run = useCallback(
    async (fn: () => Promise<void>) => {
      setBusy(true);
      setError(null);
      try {
        await fn();
      } catch (err) {
        const raw = typeof err === 'string' ? err : String(err);
        setError(humanError(err));
        // An expired session has to take the app back to sign-in. Showing the
        // message over the signed-in screen leaves somebody reading "sign in
        // again" on the one screen that has no way to do it.
        if (isSessionExpired(raw)) {
          await Logout().catch(() => undefined);
          await forgetSession();
        }
      } finally {
        setBusy(false);
      }
    },
    [forgetSession],
  );

  const signOut = useCallback(
    () =>
      void run(async () => {
        await Logout();
        await forgetSession();
      }),
    [run, forgetSession],
  );

  const copy = useCallback(
    (text: string, label = 'Copied') => {
      void CopyToClipboard(text).then(() => notify(label)).catch(() => undefined);
    },
    [notify],
  );

  const connected = status !== null;
  const phase: Phase = connected
    ? status.state === 'dropped'
      ? 'dropped'
      : status.state === 'handshaking'
        ? 'handshaking'
        : 'active'
    : busy
      ? 'handshaking'
      : 'idle';

  const target = networks.length === 1 ? networks[0] : networks.find((n) => n.id === lastNetwork);

  const connectTo = (n: agent.Network) =>
    run(async () => {
      setLastNetwork(n.id);
      setHistory([]);
      await Connect(n.id);
    });

  const disconnect = () =>
    run(async () => {
      await Disconnect();
      setStatus(null);
      setHistory([]);
    });

  const toggle = () => {
    if (connected) return void disconnect();
    if (target) void connectTo(target);
  };

  if (!session) {
    return (
      <div className="grid h-full place-items-center bg-deck-900">
        <span className="eyebrow">Initialising</span>
      </div>
    );
  }

  if (server && !server.chosen) {
    return (
      <ChooseRole
        busy={busy}
        onChoose={(host) =>
          run(async () => {
            await SetServerRole(host);
            setServer(await GetLocalServer());
          })
        }
        error={error}
        onDismissError={() => setError(null)}
      />
    );
  }

  if (!session.loggedIn) {
    return (
      <Access
        busy={busy}
        run={run}
        error={error}
        server={server}
        onDismissError={() => setError(null)}
        onDone={refreshSession}
        defaultServer={session.serverUrl}
      />
    );
  }

  return (
    <div className="flex h-full flex-col bg-deck-900">
      {/* Title bar doubles as the drag handle and carries the menus. */}
      <header
        className="flex h-8 shrink-0 items-center gap-2 border-b border-deck-line bg-deck-800 pl-2.5 pr-1"
        style={{ ['--wails-draggable' as string]: 'drag' }}
      >
        <span className="mr-1 font-mono text-[10.5px] font-semibold tracking-[0.2em] uppercase text-ink">
          Nexus<span className="text-live">VPN</span>
        </span>

        <MenuBar
          menus={[
            {
              label: 'System',
              items: [
                item('Change name…', () => setModal({ kind: 'rename' })),
                item('Settings…', () => setModal({ kind: 'settings' })),
                separator,
                item('Sign out', signOut),
                item('Exit', () => Quit()),
              ],
            },
            {
              label: 'Network',
              items: [
                item('Create network…', () => setModal({ kind: 'create' }), { hint: 'Ins' }),
                item('Join network…', () => setModal({ kind: 'join' }), { hint: '+' }),
                separator,
                item('Disconnect', disconnect, { disabled: !connected }),
              ],
            },
          ]}
        />

        <span
          className="ml-auto mr-1.5 flex items-center gap-1.5 font-mono text-[10px] tracking-[0.14em] uppercase"
          style={{ color: STATE_COLOR[phase] }}
        >
          <motion.span
            className="inline-block h-[5px] w-[5px] rounded-full"
            style={{ background: STATE_COLOR[phase] }}
            animate={
              phase === 'active' || phase === 'dropped' ? { opacity: [1, 0.3, 1] } : { opacity: 1 }
            }
            transition={{ duration: phase === 'dropped' ? 0.8 : 2.6, repeat: Infinity }}
          />
          {STATE_WORD[phase]}
        </span>
      </header>

      {/* Identity strip: the switch, who you are, and where you are. */}
      <section className="relative flex shrink-0 items-center gap-4 overflow-hidden border-b border-deck-line bg-deck-800/60 px-4 py-3">
        <DotField live={phase === 'active'} />

        <ReactorCore
          phase={phase}
          size={92}
          label={CORE_LABEL[phase]}
          disabled={busy || (!connected && !target)}
          onClick={toggle}
        />

        <div className="z-10 min-w-0 flex-1">
          <div className="truncate text-[15px] font-medium text-ink">{session.deviceName}</div>
          {/* The address takes the state's colour too. Left cyan while the
              tunnel is lost it would read as "all fine" next to a crimson
              core, which is the one moment the strip must not disagree with
              itself. */}
          <Scrambler
            value={status?.virtualIp ?? ''}
            active={phase === 'active'}
            className="block font-mono text-[19px] leading-tight tracking-[0.04em]"
            style={{ color: STATE_COLOR[phase] }}
          />
          <div className="mt-1 flex flex-wrap items-center gap-1.5">
            <span
              className="border px-1.5 py-[1px] font-mono text-[9.5px] tracking-[0.12em] uppercase"
              style={{ color: STATE_COLOR[phase], borderColor: STATE_COLOR[phase] }}
            >
              {STATE_WORD[phase]}
            </span>
            {status?.exitNodeId && (
              <span className="border border-live/40 px-1.5 py-[1px] font-mono text-[9.5px] tracking-[0.12em] uppercase text-live">
                exit route
              </span>
            )}
            {status?.killSwitchEngaged && (
              <span className="flex items-center gap-1 border border-fail/45 px-1.5 py-[1px] font-mono text-[9.5px] tracking-[0.12em] uppercase text-fail">
                <ShieldAlert size={9} aria-hidden="true" /> locked
              </span>
            )}
          </div>
        </div>
      </section>

      <AnimatePresence>
        {error && (
          <motion.div
            initial={{ height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: 0, opacity: 0 }}
            className="flex shrink-0 items-start gap-2.5 border-b border-fail/40 bg-fail/10 px-3 py-2"
            role="alert"
          >
            <AlertTriangle size={13} className="mt-0.5 shrink-0 text-fail" aria-hidden="true" />
            <div className="min-w-0 flex-1">
              <p className="text-[12px] text-ink">{error.message}</p>
              {error.fix && <p className="mt-0.5 text-[11.5px] text-ink-dim">{error.fix}</p>}
            </div>
            <button
              onClick={() => setError(null)}
              className="text-ink-faint hover:text-ink"
              aria-label="Dismiss"
            >
              <X size={13} />
            </button>
          </motion.div>
        )}
      </AnimatePresence>

      <NetworkTree
        networks={networks}
        peers={status?.peers ?? []}
        activeNetworkId={status?.networkId ?? ''}
        exitNodeId={status?.exitNodeId ?? ''}
        selfName={session.deviceName}
        busy={busy}
        connected={connected}
        onConnect={connectTo}
        onDisconnect={disconnect}
        onShare={(network) => setModal({ kind: 'invite', network })}
        onPairPhone={(network) => setModal({ kind: 'pair', network })}
        onCopy={copy}
        onUseExit={(peer) =>
          run(() =>
            UseExitNode(peer.deviceId, {
              killSwitch: killSwitchPref === 'on',
              allowLan: allowLanPref === 'on',
            }),
          )
        }
        onStopExit={() => run(() => StopUsingExitNode())}
        onProperties={(peer) => setModal({ kind: 'properties', peer })}
      />

      <StatusStrip connected={connected} history={history} activeSince={status?.activeSince ?? ''} />

      {/* Dialogs */}
      {modal?.kind === 'create' && (
        <CreateDialog
          busy={busy}
          onClose={() => setModal(null)}
          onCreate={(name) =>
            run(async () => {
              await CreateNetwork(name, '', '');
              setModal(null);
              await refreshNetworks();
            })
          }
        />
      )}
      {modal?.kind === 'join' && (
        <JoinDialog
          busy={busy}
          initialCode={modal.code}
          onClose={() => setModal(null)}
          onJoin={(code) =>
            run(async () => {
              await JoinNetwork(code);
              setModal(null);
              await refreshNetworks();
            })
          }
        />
      )}
      {modal?.kind === 'rename' && (
        <RenameDialog
          current={session.deviceName}
          busy={busy}
          onClose={() => setModal(null)}
          onSave={(name) =>
            run(async () => {
              await SetDeviceName(name);
              setModal(null);
              await refreshSession();
            })
          }
        />
      )}
      {modal?.kind === 'settings' && (
        <SettingsDialog
          session={session}
          version={version}
          busy={busy}
          server={server}
          logs={logs}
          killSwitch={killSwitchPref === 'on'}
          allowLan={allowLanPref === 'on'}
          onKillSwitch={(v) => setKillSwitchPref(v ? 'on' : 'off')}
          onAllowLan={(v) => setAllowLanPref(v ? 'on' : 'off')}
          onRotate={() => run(async () => void (await RotateDeviceKey()))}
          onCopy={copy}
          onClose={() => setModal(null)}
        />
      )}
      {modal?.kind === 'properties' && (
        <PropertiesDialog
          peer={modal.peer}
          networkName={networks.find((n) => n.id === status?.networkId)?.name ?? ''}
          onCopy={copy}
          onClose={() => setModal(null)}
        />
      )}
      {modal?.kind === 'pair' && (
        <PairPhone network={modal.network} onClose={() => setModal(null)} />
      )}
      {modal?.kind === 'invite' && (
        <Invite network={modal.network} onClose={() => setModal(null)} onCopy={copy} />
      )}

      <AnimatePresence>
        {toast && (
          <motion.div
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: 8 }}
            className="pointer-events-none fixed bottom-9 left-1/2 z-50 -translate-x-1/2 border border-live/40 bg-deck-800 px-3.5 py-1.5 font-mono text-[10.5px] tracking-[0.1em] uppercase text-live"
          >
            {toast}
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

/* ============================================================
   Status strip
   ============================================================ */

function StatusStrip({
  connected,
  history,
  activeSince,
}: {
  connected: boolean;
  history: Sample[];
  activeSince: string;
}) {
  const [open, setOpen] = useState(false);

  return (
    <div className="shrink-0 border-t border-deck-line bg-deck-800">
      <button
        onClick={() => setOpen((v) => !v)}
        disabled={!connected}
        className="flex w-full items-center gap-3 px-3 py-1.5 text-left disabled:cursor-default"
      >
        <span className="eyebrow">Telemetry</span>
        {connected ? (
          <>
            <Inline label="rtt" value={fmtLatency(history.at(-1)?.latencyMs ?? -1)} />
            <Inline label="down" value={rate(history.at(-1)?.recvDelta ?? 0)} />
            <Inline label="up" value={rate(history.at(-1)?.sentDelta ?? 0)} />
            <span className="ml-auto font-mono text-[10px] text-ink-faint">
              {elapsed(activeSince)}
            </span>
          </>
        ) : (
          <span className="font-mono text-[10px] text-ink-faint">idle</span>
        )}
      </button>

      <AnimatePresence>
        {open && connected && (
          <motion.div
            initial={{ height: 0 }}
            animate={{ height: 'auto' }}
            exit={{ height: 0 }}
            className="overflow-hidden border-t border-deck-line"
          >
            <div className="p-3">
              <Telemetry history={history} />
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  );
}

function Inline({ label, value }: { label: string; value: string }) {
  return (
    <span className="flex items-baseline gap-1 font-mono text-[10.5px]">
      <span className="text-ink-faint">{label}</span>
      <span className="text-ink-dim">{value}</span>
    </span>
  );
}

/* ============================================================
   Dialogs
   ============================================================ */

function CreateDialog({
  busy,
  onClose,
  onCreate,
}: {
  busy: boolean;
  onClose: () => void;
  onCreate: (name: string) => void;
}) {
  const [name, setName] = useState('');
  return (
    <Dialog
      title="Create network"
      onClose={onClose}
      footer={
        <>
          <Secondary onClick={onClose}>Cancel</Secondary>
          <Primary disabled={busy || !name.trim()} onClick={() => onCreate(name)}>
            Create
          </Primary>
        </>
      }
    >
      <form
        className="p-4"
        onSubmit={(e) => {
          e.preventDefault();
          if (name.trim()) onCreate(name);
        }}
      >
        <Row label="Network name">
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Home Lab" required />
        </Row>
        {/* No password field. Radmin gates a network on a shared secret
            everyone types; here membership is an account on your control
            plane and the invite code is single-purpose, so a password
            would be a second, weaker credential for the same door. */}
        <p className="mt-1 text-[11.5px] leading-relaxed text-ink-faint">
          The address range, routing and keys are handled for you. Invite people afterwards with
          a code or a QR — no shared password to circulate or change.
        </p>
      </form>
    </Dialog>
  );
}

function JoinDialog({
  busy,
  initialCode,
  onClose,
  onJoin,
}: {
  busy: boolean;
  initialCode?: string;
  onClose: () => void;
  onJoin: (code: string) => void;
}) {
  const [code, setCode] = useState(initialCode ?? '');
  return (
    <Dialog
      title="Join network"
      onClose={onClose}
      footer={
        <>
          <Secondary onClick={onClose}>Cancel</Secondary>
          <Primary disabled={busy || !code.trim()} onClick={() => onJoin(code)}>
            Join
          </Primary>
        </>
      }
    >
      <form
        className="p-4"
        onSubmit={(e) => {
          e.preventDefault();
          if (code.trim()) onJoin(code);
        }}
      >
        <Row label="Invite">
          <Input
            value={code}
            onChange={(e) => setCode(e.target.value)}
            placeholder="K7M2QP"
            required
          />
        </Row>
        <p className="mt-1 text-[11.5px] leading-relaxed text-ink-faint">
          Paste the code or the whole link you were sent — either works.
        </p>
      </form>
    </Dialog>
  );
}

function RenameDialog({
  current,
  busy,
  onClose,
  onSave,
}: {
  current: string;
  busy: boolean;
  onClose: () => void;
  onSave: (name: string) => void;
}) {
  const [name, setName] = useState(current);
  return (
    <Dialog
      title="Change name"
      onClose={onClose}
      footer={
        <>
          <Secondary onClick={onClose}>Cancel</Secondary>
          <Primary disabled={busy || !name.trim()} onClick={() => onSave(name)}>
            Save
          </Primary>
        </>
      }
    >
      <form
        className="p-4"
        onSubmit={(e) => {
          e.preventDefault();
          if (name.trim()) onSave(name);
        }}
      >
        <Row label="This machine">
          <Input value={name} onChange={(e) => setName(e.target.value)} required />
        </Row>
        <p className="mt-1 text-[11.5px] text-ink-faint">The name everyone else sees.</p>
      </form>
    </Dialog>
  );
}

function PropertiesDialog({
  peer,
  networkName,
  onCopy,
  onClose,
}: {
  peer: agent.Peer;
  networkName: string;
  onCopy: (text: string, label?: string) => void;
  onClose: () => void;
}) {
  return (
    <Dialog
      title={`Properties: ${peer.deviceName || 'machine'}`}
      onClose={onClose}
      width={430}
      footer={
        <>
          <Secondary onClick={() => onCopy(peer.virtualIp, 'Address copied')}>
            Copy address
          </Secondary>
          <Primary onClick={onClose}>Close</Primary>
        </>
      }
    >
      <div className="p-4">
        <Group title="Machine">
          <Fact label="Name" value={peer.deviceName || '—'} />
          <Fact label="Platform" value={peer.os || 'unknown'} />
          <Fact label="Address" value={peer.virtualIp || '—'} mono />
          <Fact label="Device ID" value={peer.deviceId} mono small />
        </Group>

        <Group title="Network">
          <Fact label="Network" value={networkName || '—'} />
          <Fact label="Offers exit" value={peer.exitNode ? 'yes' : 'no'} />
        </Group>

        <Group title="Connection">
          <Fact label="Status" value={peer.quality} />
          <Fact label="Path" value={PATH_WORD[peer.mode] ?? peer.mode ?? '—'} />
          <Fact label="Round trip" value={peer.latencyMs >= 0 ? `${peer.latencyMs} ms` : 'not measured'} mono />
          <Fact label="Endpoint" value={peer.endpoint || '—'} mono small />
          <Fact label="Last handshake" value={peer.lastHandshake ? new Date(peer.lastHandshake).toLocaleString() : 'never'} />
          <Fact label="Transferred" value={`${bytes(peer.bytesSent)} up · ${bytes(peer.bytesReceived)} down`} mono />
        </Group>
      </div>
    </Dialog>
  );
}

function Group({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <fieldset className="mb-3 rounded-[2px] border border-deck-line px-3 pb-2.5 pt-1 last:mb-0">
      <legend className="eyebrow px-1">{title}</legend>
      {children}
    </fieldset>
  );
}

function Fact({
  label,
  value,
  mono,
  small,
}: {
  label: string;
  value: string;
  mono?: boolean;
  small?: boolean;
}) {
  return (
    <div className="flex items-baseline gap-3 py-[3px]">
      <span className="w-[104px] shrink-0 text-[11.5px] text-ink-faint">{label}</span>
      <span
        className={`min-w-0 flex-1 break-all ${mono ? 'font-mono' : ''} ${
          small ? 'text-[10.5px]' : 'text-[12px]'
        } text-ink-dim`}
      >
        {value}
      </span>
    </div>
  );
}

/**
 * Whether this machine can be reached from outside the house.
 *
 * The three states are genuinely different and are shown as such: never
 * asked, asked and the router agreed, asked and the router refused. Collapsing
 * the last two into "not working" would leave somebody unable to tell whether
 * to change a setting here or one on their router.
 */
function Reach({ server }: { server: main.LocalServer | null }) {
  const [remote, setRemote] = useState(server?.remote ?? false);
  const [relay, setRelay] = useState(server?.relay ?? false);
  const [saved, setSaved] = useState(false);

  if (!server?.host) {
    return (
      <p className="text-[11.5px] leading-relaxed text-ink-dim">
        This machine joins a server somebody else runs, so there is nothing here
        to expose. These settings belong on the machine hosting the network.
      </p>
    );
  }

  const change = (v: boolean) => {
    setRemote(v);
    setSaved(false);
    SetReachableFromAnywhere(v)
      .then(() => setSaved(true))
      .catch(() => undefined);
  };

  return (
    <>
      <Group title="Away from this network">
        <Toggle
          checked={remote}
          onChange={change}
          label={remote ? 'Reachable from anywhere' : 'This network only'}
        />
        <p className="mt-1.5 text-[11.5px] leading-relaxed text-ink-faint">
          Asks your router to forward a port, so your phone can connect over
          mobile data. Leave it off if your devices are only ever on the same
          Wi-Fi &mdash; it puts this machine on the internet, and the fewer
          things there the better.
        </p>
        {saved && (
          <p className="mt-1.5 text-[11.5px] leading-relaxed text-work">
            Takes effect the next time NexusVPN starts.
          </p>
        )}
      </Group>

      <Group title="When two machines cannot reach each other">
        <Toggle
          checked={relay}
          onChange={(v) => {
            setRelay(v);
            setSaved(false);
            SetRelayHere(v).then(() => setSaved(true)).catch(() => undefined);
          }}
          label={relay ? 'This machine relays' : 'No relaying'}
        />
        <p className="mt-1.5 text-[11.5px] leading-relaxed text-ink-faint">
          Two machines usually connect directly, by punching through both their
          routers. Some networks &mdash; mobile networks especially &mdash;
          make that impossible, and those pairs simply cannot talk. With this
          on, their traffic goes through here instead. It stays encrypted end
          to end; this machine carries it without being able to read it.
        </p>
      </Group>

      <Group title="Addresses">
        <Fact label="On this network" value={server.lanUrl || 'none found'} small />
        {remote && (
          <Fact
            label="From anywhere"
            value={server.publicUrl || 'the router did not open a port'}
            small
          />
        )}
      </Group>

      {remote && !server.publicUrl && server.running && (
        <p className="text-[11.5px] leading-relaxed text-ink-dim">
          Your router refused, or cannot be asked. Most routers call this{' '}
          <strong className="text-ink">UPnP</strong> or{' '}
          <strong className="text-ink">NAT-PMP</strong>, and many ship with it
          switched off. Turning it on there does the same job. Forwarding by
          hand works too &mdash; to this machine, at {server.lanUrl || 'its address above'}:
        </p>
      )}
      {remote && !server.publicUrl && server.running && (
        <p className="mt-1.5 text-[11.5px] leading-relaxed text-ink-faint">
          <strong className="text-ink">TCP 8080</strong> to sign in,{' '}
          <strong className="text-ink">TCP 9090</strong> for the tunnel to
          negotiate{relay ? ', ' : '. '}
          {relay && (
            <>
              and <strong className="text-ink">UDP 51821</strong> for relaying.{' '}
            </>
          )}
          Peer-to-peer traffic needs none of them &mdash; it finds its own way
          through.
        </p>
      )}
    </>
  );
}

function SettingsDialog({
  session,
  version,
  busy,
  server,
  logs,
  killSwitch,
  allowLan,
  onKillSwitch,
  onAllowLan,
  onRotate,
  onCopy,
  onClose,
}: {
  session: agent.Session;
  version: string;
  busy: boolean;
  server: main.LocalServer | null;
  logs: string[];
  killSwitch: boolean;
  allowLan: boolean;
  onKillSwitch: (v: boolean) => void;
  onAllowLan: (v: boolean) => void;
  onRotate: () => void;
  onCopy: (text: string, label?: string) => void;
  onClose: () => void;
}) {
  const [tab, setTab] = useState<'general' | 'reach' | 'routing' | 'diagnostics'>('general');
  const [startup, setStartup] = useState<main.StartupPref | null>(null);
  const [startupErr, setStartupErr] = useState('');

  useEffect(() => {
    GetStartWithSystem().then(setStartup).catch(() => setStartup(null));
  }, []);

  const changeStartup = (on: boolean) => {
    // Optimistic, then re-read: the answer that matters is what is actually
    // registered with the system, not what was asked for.
    setStartup((p) => (p ? { ...p, enabled: on } : p));
    setStartupErr('');
    SetStartWithSystem(on)
      .catch((e) => setStartupErr(String(e)))
      .finally(() => {
        GetStartWithSystem().then(setStartup).catch(() => {});
      });
  };

  return (
    <Dialog
      title="Settings"
      onClose={onClose}
      width={560}
      footer={<Primary onClick={onClose}>Done</Primary>}
    >
      <div className="flex min-h-[300px]">
        <nav className="w-[150px] shrink-0 border-r border-deck-line py-2">
          {(
            [
              ['general', 'General', Sliders],
              ['reach', 'Reach', Globe],
              ['routing', 'Routing', ShieldAlert],
              ['diagnostics', 'Diagnostics', Bell],
            ] as const
          ).map(([id, label, Icon]) => (
            <button
              key={id}
              onClick={() => setTab(id)}
              className={`flex w-full items-center gap-2.5 px-3 py-2 text-left text-[12px] transition-colors ${
                tab === id ? 'bg-live/12 text-live' : 'text-ink-dim hover:text-ink'
              }`}
            >
              <Icon size={13} aria-hidden="true" />
              {label}
            </button>
          ))}
        </nav>

        <div className="min-w-0 flex-1 p-4">
          {tab === 'general' && (
            <>
              <Group title="Account">
                <Fact label="Signed in as" value={session.email} />
                <Fact label="Server" value={session.serverUrl} small />
                <Fact label="This machine" value={session.deviceName} />
                <Fact label="Version" value={version} mono />
              </Group>
              {startup?.supported && (
                <Group title="Starting up">
                  <Toggle
                    checked={startup.enabled}
                    onChange={changeStartup}
                    label={
                      startup.enabled
                        ? 'Starts when you sign in'
                        : 'Does not start when you sign in'
                    }
                  />
                  <p className="mt-1.5 text-[11.5px] leading-relaxed text-ink-faint">
                    Opens straight to the notification area, not a window. Worth leaving on if
                    this machine is the server &mdash; nobody else can reach the network while it
                    is not running.
                  </p>
                  {startupErr && (
                    <p className="mt-1.5 text-[11.5px] leading-relaxed text-fail">{startupErr}</p>
                  )}
                </Group>
              )}
              <Group title="Device key">
                <p className="mb-2 text-[11.5px] leading-relaxed text-ink-dim">
                  The private half never leaves this machine. Rotating disconnects the tunnel and
                  retires the old key everywhere once this machine signs back in.
                </p>
                <div className="mb-2 overflow-x-auto rounded-[2px] border border-deck-line bg-deck-900 px-2 py-1.5 font-mono text-[10.5px] text-ink-dim">
                  {session.publicKey}
                </div>
                <div className="flex gap-1.5">
                  <Secondary onClick={() => onCopy(session.publicKey, 'Public key copied')}>
                    Copy
                  </Secondary>
                  <Danger disabled={busy} onClick={onRotate}>
                    <span className="flex items-center gap-1">
                      <KeyRound size={11} /> Rotate
                    </span>
                  </Danger>
                </div>
              </Group>
            </>
          )}

          {tab === 'reach' && <Reach server={server} />}

          {tab === 'routing' && (
            <>
              <p className="mb-3 text-[11.5px] leading-relaxed text-ink-dim">
                These apply when you route all of this machine&rsquo;s traffic through another
                machine — right-click one that offers it. They do nothing on an ordinary mesh
                connection, where each machine only carries its own address.
              </p>
              <div className="mb-3">
                <Toggle
                  checked={killSwitch}
                  onChange={onKillSwitch}
                  label={killSwitch ? 'Kill switch on' : 'Kill switch off'}
                />
                <p className="mt-1.5 text-[11.5px] leading-relaxed text-ink-faint">
                  If the tunnel drops while it is carrying everything, block traffic rather than
                  letting it fall back to your ordinary connection in the clear.
                </p>
              </div>
              <div>
                <Toggle
                  checked={allowLan}
                  onChange={onAllowLan}
                  label={allowLan ? 'Local network allowed' : 'Local network blocked'}
                />
                <p className="mt-1.5 text-[11.5px] leading-relaxed text-ink-faint">
                  Keep your printer, NAS and router reachable while blocked. Turn this off only on
                  a network you do not trust, where the machines around you are the point.
                </p>
              </div>
            </>
          )}

          {tab === 'diagnostics' && (
            <>
              <span className="eyebrow mb-2 block">Engine log</span>
              <pre className="max-h-[240px] overflow-auto rounded-[2px] border border-deck-line bg-deck-900 p-2 font-mono text-[10.5px] leading-relaxed whitespace-pre-wrap text-ink-faint">
                {logs.slice(-120).join('\n') || 'Nothing yet.'}
              </pre>
              <div className="mt-2">
                <Secondary onClick={() => onCopy(logs.join('\n'), 'Log copied')}>
                  Copy log
                </Secondary>
              </div>
            </>
          )}
        </div>
      </div>
    </Dialog>
  );
}

/* ============================================================
   First run: what is this machine for?
   ============================================================ */

/**
 * Asked once, before anything binds a port.
 *
 * Both answers are correct and only the person installing knows which they
 * mean, so it is a question rather than something inferred. Inferring it —
 * hosting whenever port 8080 happens to be free, say — would leave an office
 * of ten machines quietly running ten servers, each with its own accounts,
 * none able to see the others.
 */
function ChooseRole({
  busy,
  error,
  onChoose,
  onDismissError,
}: {
  busy: boolean;
  error: { message: string; fix?: string } | null;
  onChoose: (host: boolean) => void;
  onDismissError: () => void;
}) {
  return (
    <div className="grid-field grid h-full place-items-center bg-deck-900 p-8">
      <motion.div
        initial={{ opacity: 0, y: 10 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.3 }}
        className="panel ticked w-full max-w-[420px] p-6"
      >
        <p className="font-mono text-[13px] font-semibold tracking-[0.22em] uppercase text-ink">
          Nexus<span className="text-live">VPN</span>
        </p>
        <p className="mb-5 mt-1.5 text-[12px] leading-relaxed text-ink-dim">
          One machine holds the accounts and networks, and the others connect to it.
          Which is this?
        </p>

        {error && (
          <div className="mb-4 flex items-start gap-2.5 border border-fail/45 bg-fail/10 p-2.5" role="alert">
            <AlertTriangle size={13} className="mt-0.5 shrink-0 text-fail" aria-hidden="true" />
            <div className="min-w-0 flex-1">
              <p className="text-[12px] text-ink">{error.message}</p>
              {error.fix && <p className="mt-0.5 text-[11.5px] text-ink-dim">{error.fix}</p>}
            </div>
            <button onClick={onDismissError} className="text-ink-faint hover:text-ink" aria-label="Dismiss">
              <X size={12} />
            </button>
          </div>
        )}

        <div className="grid gap-2.5">
          <RoleCard
            title="This machine is the server"
            body="Accounts and networks live here. Other machines join this one. It needs to be switched on for them to connect."
            action="Host here"
            busy={busy}
            onClick={() => onChoose(true)}
            primary
          />
          <RoleCard
            title="Join a server someone else runs"
            body="Somebody has already set one up — a colleague's machine, or your own on another PC. You will need its address."
            action="Join one"
            busy={busy}
            onClick={() => onChoose(false)}
          />
        </div>

        <p className="mt-4 text-[11px] leading-relaxed text-ink-faint">
          You can change this later; it is not a decision you are stuck with.
        </p>
      </motion.div>
    </div>
  );
}

function RoleCard({
  title,
  body,
  action,
  busy,
  primary,
  onClick,
}: {
  title: string;
  body: string;
  action: string;
  busy: boolean;
  primary?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={busy}
      className={`ticked group border p-3.5 text-left transition-colors disabled:opacity-50 ${
        primary
          ? 'border-live/45 bg-live/8 hover:bg-live/14'
          : 'border-deck-line hover:border-deck-line-bright hover:bg-deck-700/50'
      }`}
    >
      <div className="flex items-center gap-2">
        {primary ? (
          <Server size={14} className="shrink-0 text-live" aria-hidden="true" />
        ) : (
          <Link2 size={14} className="shrink-0 text-ink-dim" aria-hidden="true" />
        )}
        <span className={`text-[13px] font-medium ${primary ? 'text-live' : 'text-ink'}`}>
          {title}
        </span>
      </div>
      <p className="mt-1.5 text-[11.5px] leading-relaxed text-ink-faint">{body}</p>
      <span
        className={`mt-2.5 inline-block font-mono text-[10px] tracking-[0.14em] uppercase ${
          primary ? 'text-live' : 'text-ink-dim'
        }`}
      >
        {action} →
      </span>
    </button>
  );
}

/* ============================================================
   Access
   ============================================================ */

function Access({
  busy,
  run,
  error,
  server,
  onDismissError,
  onDone,
  defaultServer,
}: {
  busy: boolean;
  run: (fn: () => Promise<void>) => Promise<void>;
  error: { message: string; fix?: string } | null;
  server: main.LocalServer | null;
  onDismissError: () => void;
  onDone: () => Promise<void>;
  defaultServer: string;
}) {
  // With the built-in server running there is nothing to configure, and on
  // a fresh install there is nobody to sign in as — so this screen asks to
  // create an account and never mentions an address at all. The field
  // appears only for somebody pointing at a server they did not start.
  const local = server?.running ?? false;
  const joining = (server?.chosen ?? false) && !(server?.host ?? false);
  const [creating, setCreating] = useState(server?.firstRun ?? false);
  const [address, setAddress] = useState(defaultServer || server?.url || DEFAULT_SERVER);
  const [showServer, setShowServer] = useState(joining || (!local && defaultServer === ''));
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [mfa, setMfa] = useState('');
  const [needsMfa, setNeedsMfa] = useState(false);

  useEffect(() => {
    if (!server?.running) return;
    setCreating(server.firstRun);
    if (!defaultServer) setAddress(server.url);
  }, [server, defaultServer]);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    void run(async () => {
      if (creating) {
        await Register(address, email, password, name || email);
        await onDone();
        return;
      }
      try {
        await Login(address, email, password, mfa);
      } catch (err) {
        if (String(err).includes('mfa_required')) {
          setNeedsMfa(true);
          return;
        }
        throw err;
      }
      await onDone();
    });
  };

  return (
    <div className="grid-field grid h-full place-items-center bg-deck-900 p-8">
      <motion.form
        onSubmit={submit}
        initial={{ opacity: 0, y: 10 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.3 }}
        className="panel ticked w-full max-w-[330px] p-6"
      >
        <p className="font-mono text-[13px] font-semibold tracking-[0.22em] uppercase text-ink">
          Nexus<span className="text-live">VPN</span>
        </p>
        <p className="mb-5 mt-1.5 text-[12px] text-ink-dim">
          {creating
            ? 'Set up an account on this machine. It stays here — nothing is sent anywhere.'
            : 'Your machines, on one network, wherever they are.'}
        </p>

        <AnimatePresence>
          {error && (
            <motion.div
              initial={{ opacity: 0, height: 0 }}
              animate={{ opacity: 1, height: 'auto' }}
              exit={{ opacity: 0, height: 0 }}
              className="mb-4 flex items-start gap-2.5 border border-fail/45 bg-fail/10 p-2.5"
              role="alert"
            >
              <AlertTriangle size={13} className="mt-0.5 shrink-0 text-fail" aria-hidden="true" />
              <div className="min-w-0 flex-1">
                <p className="text-[12px] text-ink">{error.message}</p>
                {error.fix && <p className="mt-0.5 text-[11.5px] text-ink-dim">{error.fix}</p>}
              </div>
              <button
                onClick={onDismissError}
                className="text-ink-faint hover:text-ink"
                aria-label="Dismiss"
              >
                <X size={12} />
              </button>
            </motion.div>
          )}
        </AnimatePresence>

        <div className="grid gap-3">
          {creating && (
            <label className="grid gap-1">
              <span className="eyebrow">Your name</span>
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="Jithesh" />
            </label>
          )}
          <label className="grid gap-1">
            <span className="eyebrow">Email</span>
            <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required autoFocus />
          </label>
          <label className="grid gap-1">
            <span className="eyebrow">Password</span>
            <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required />
          </label>

          <AnimatePresence>
            {needsMfa && (
              <motion.label
                className="grid gap-1"
                initial={{ opacity: 0, height: 0 }}
                animate={{ opacity: 1, height: 'auto' }}
              >
                <span className="eyebrow">Authentication code</span>
                <Input value={mfa} onChange={(e) => setMfa(e.target.value)} placeholder="123456" required autoFocus />
              </motion.label>
            )}
          </AnimatePresence>

          {showServer ? (
            <label className="grid gap-1">
              <span className="eyebrow">Server</span>
              <Input
                value={address}
                onChange={(e) => setAddress(e.target.value)}
                placeholder={DEFAULT_SERVER}
                required
              />
              <span className="text-[11px] leading-relaxed text-ink-faint">
                Where the NexusVPN server is running.
              </span>
            </label>
          ) : (
            <button
              type="button"
              onClick={() => setShowServer(true)}
              className="justify-self-start font-mono text-[10px] tracking-[0.12em] uppercase text-ink-faint transition-colors hover:text-live"
            >
              use a different server
            </button>
          )}

          <button
            type="submit"
            disabled={busy}
            className="mt-1 border border-live/50 bg-live/12 py-2 font-mono text-[11px] font-semibold tracking-[0.16em] uppercase text-live transition-colors hover:bg-live/22 disabled:opacity-40"
          >
            {busy ? (creating ? 'creating' : 'authenticating') : creating ? 'create account' : 'sign in'}
          </button>

          {/* The other mode is always one click away: a returning user on a
              machine whose store was wiped needs sign-in, and somebody
              joining a colleague's server needs create-account. */}
          <button
            type="button"
            onClick={() => setCreating((v: boolean) => !v)}
            className="font-mono text-[10px] tracking-[0.12em] uppercase text-ink-faint transition-colors hover:text-live"
          >
            {creating ? 'i already have an account' : 'create an account'}
          </button>

          {local && server?.lanUrl && (
            <p className="mt-1 text-[11px] leading-relaxed text-ink-faint">
              Other machines on your network join this one at{' '}
              <span className="font-mono text-ink-dim">{server.lanUrl}</span>
            </p>
          )}
        </div>
      </motion.form>
    </div>
  );
}

/* ============================================================
   Helpers
   ============================================================ */

/** Ambient dot matrix. Atmosphere only, so it settles when nothing is live. */
function DotField({ live }: { live: boolean }) {
  return (
    <motion.div
      aria-hidden="true"
      className="pointer-events-none absolute inset-0"
      style={{
        backgroundImage:
          'radial-gradient(circle, color-mix(in oklab, var(--color-rest) 50%, transparent) 1px, transparent 1px)',
        backgroundSize: '18px 18px',
        maskImage: 'radial-gradient(ellipse at 8% 50%, #000 0%, transparent 65%)',
        WebkitMaskImage: 'radial-gradient(ellipse at 8% 50%, #000 0%, transparent 65%)',
      }}
      animate={{ opacity: live ? [0.3, 0.5, 0.3] : 0.18 }}
      transition={{ duration: 5, repeat: Infinity, ease: 'easeInOut' }}
    />
  );
}

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

/**
 * Whether an error means the stored session is no longer good for anything.
 *
 * Separate from humanError because it drives behaviour, not wording: the app
 * has to leave the signed-in screen, and matching on a rendered sentence to
 * decide that would break the moment the sentence changed.
 */
function isSessionExpired(raw: string): boolean {
  const lower = raw.toLowerCase();
  if (lower.includes('invalid credentials') || lower.includes('incorrect password')) {
    return false;
  }
  return (
    lower.includes('unauthorized') ||
    lower.includes('not signed in') ||
    lower.includes('session expired') ||
    lower.includes('token')
  );
}

/** Turns an error from the Go bridge into something a person can act on. */
function humanError(err: unknown): { message: string; fix?: string } {
  const raw = typeof err === 'string' ? err : err instanceof Error ? err.message : String(err);
  const lower = raw.toLowerCase();

  if (lower.includes('administrator') || lower.includes('root')) {
    return {
      message: 'NexusVPN needs permission to create a tunnel interface.',
      fix: 'Quit and reopen as an administrator. Nothing else on this machine changes.',
    };
  }
  if (lower.includes('exit node') && lower.includes('not supported')) {
    return {
      message: 'This machine cannot act as an exit node yet.',
      fix: 'Routing through someone else works everywhere; only offering to be one is limited.',
    };
  }
  if (lower.includes('default route')) {
    return {
      message: 'Routing everything through a peer needs a working internet connection to route around.',
      fix: 'Reconnect to a network and try again.',
    };
  }
  if (lower.includes('invalid credentials') || lower.includes('incorrect password')) {
    return {
      message: 'That email and password do not match an account on this server.',
      fix: 'Check the server address too — accounts belong to one server, not all of them.',
    };
  }
  if (isSessionExpired(raw)) {
    return { message: 'Your session has expired.', fix: 'Sign in again to continue.' };
  }
  if (lower.includes('permissiondenied') || lower.includes('forbidden')) {
    return {
      message: 'This account is not a member of that network.',
      fix: 'Ask whoever runs it for a fresh invite code, or sign in as the account that joined it.',
    };
  }
  if (lower.includes('invite')) {
    return { message: 'That invite did not work.', fix: 'Codes can be replaced — ask for a fresh one.' };
  }
  // Windows' wording for a TLS listener hanging up on a plaintext request,
  // which is what an installation that predates TLS does to itself.
  if (lower.includes('forcibly closed') || lower.includes('wsarecv')) {
    return {
      message: 'The server closed the connection before answering.',
      fix: 'Usually an address left over from an older version. Sign out and back in, or take a fresh pairing code from the machine hosting the network.',
    };
  }
  if (lower.includes('not the machine you paired with')) {
    return {
      message: 'Something else is answering at that address.',
      fix: 'If the server was reinstalled, its certificate changed and this device needs pairing again. If it was not, do not continue.',
    };
  }
  if (lower.includes('connection refused') || lower.includes('no such host') || lower.includes('dial')) {
    return {
      message: 'Cannot reach your server.',
      fix: 'Check that you are online and the server address is right.',
    };
  }
  return { message: raw };
}

function bestLatency(peers: agent.Peer[]): number {
  const measured = peers.filter((p) => p.latencyMs >= 0).map((p) => p.latencyMs);
  return measured.length ? Math.min(...measured) : -1;
}

function fmtLatency(ms: number): string {
  return ms < 0 ? '—' : `${ms}ms`;
}

function rate(bytesPerSecond: number): string {
  if (bytesPerSecond <= 0) return '0B/s';
  const units = ['B', 'K', 'M', 'G'];
  const i = Math.min(Math.floor(Math.log(bytesPerSecond) / Math.log(1024)), units.length - 1);
  return `${(bytesPerSecond / 1024 ** i).toFixed(i === 0 ? 0 : 1)}${units[i]}/s`;
}

function bytes(n: number): string {
  if (!n) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.min(Math.floor(Math.log(n) / Math.log(1024)), units.length - 1);
  return `${(n / 1024 ** i).toFixed(i === 0 ? 0 : 1)} ${units[i]}`;
}

function elapsed(since: string): string {
  if (!since) return '—';
  const secs = Math.max(0, Math.floor((Date.now() - new Date(since).getTime()) / 1000));
  const h = Math.floor(secs / 3600);
  const m = Math.floor((secs % 3600) / 60);
  const s = secs % 60;
  return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}`;
}

const PATH_WORD: Record<string, string> = {
  direct: 'peer to peer',
  relay: 'through a relay',
  connecting: 'negotiating',
  offline: 'not connected',
};

/// The address a server started on this machine with the project's own
/// docker compose listens on. Prefilled because it is right for every local
/// test and wrong only for someone who already knows what to change it to.
const DEFAULT_SERVER = 'http://localhost:8080';

const CORE_LABEL: Record<Phase, string> = {
  idle: 'off',
  handshaking: '···',
  active: 'on',
  dropped: 'retry',
};

const STATE_WORD: Record<Phase, string> = {
  idle: 'offline',
  handshaking: 'linking',
  active: 'online',
  dropped: 'tunnel lost',
};

const STATE_COLOR: Record<Phase, string> = {
  idle: 'var(--color-rest)',
  handshaking: 'var(--color-work)',
  active: 'var(--color-live)',
  dropped: 'var(--color-fail)',
};
