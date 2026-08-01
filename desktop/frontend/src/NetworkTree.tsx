import { useEffect, useRef, useState } from 'react';
import { AnimatePresence, motion } from 'framer-motion';
import {
  ChevronDown,
  ChevronRight,
  Copy,
  Globe,
  Info,
  Link2,
  Link2Off,
  QrCode,
  Radio,
} from 'lucide-react';

import type { agent } from '../wailsjs/go/models';
import SignalBars from './SignalBars';

/**
 * Every network you belong to, with its machines nested underneath.
 *
 * This is Radmin's shape and it is the right one: a person opens this app
 * to find one machine among a handful and do something with it, and a tree
 * puts every candidate on screen at once with no navigation. Two separate
 * panels — networks here, devices there — makes you hold the relationship
 * between them in your head instead of showing it.
 *
 * The columns are Radmin's columns: status, name, address. The instrumented
 * treatment sits on top of that, never in place of it.
 */
export default function NetworkTree({
  networks,
  peers,
  activeNetworkId,
  exitNodeId,
  selfName,
  busy,
  connected,
  onConnect,
  onDisconnect,
  onShare,
  onCopy,
  onUseExit,
  onStopExit,
  onProperties,
}: {
  networks: agent.Network[];
  peers: agent.Peer[];
  activeNetworkId: string;
  exitNodeId: string;
  selfName: string;
  busy: boolean;
  connected: boolean;
  onConnect: (n: agent.Network) => void;
  onDisconnect: () => void;
  onShare: (n: agent.Network) => void;
  onCopy: (text: string, label?: string) => void;
  onUseExit: (peer: agent.Peer) => void;
  onStopExit: () => void;
  onProperties: (peer: agent.Peer) => void;
}) {
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});

  if (networks.length === 0) {
    return (
      <div className="grid-field grid flex-1 place-items-center p-10 text-center">
        <div>
          <Radio size={18} className="mx-auto mb-3 text-ink-faint" aria-hidden="true" />
          <p className="font-mono text-[11px] tracking-[0.16em] uppercase text-ink-dim">
            No networks
          </p>
          <p className="mx-auto mt-2 max-w-[40ch] text-[12px] leading-relaxed text-ink-faint">
            Create one for your own machines, or join a friend&rsquo;s with their invite code.
            Both are under the Network menu.
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="flex-1 overflow-y-auto">
      {networks.map((n) => {
        const isActive = n.id === activeNetworkId;
        const isOpen = !collapsed[n.id];
        // Peers only exist for the network the tunnel is actually on. The
        // others show their member count and nothing more, which is
        // honest: we have not talked to those machines.
        const members = isActive ? peers : [];

        return (
          <div key={n.id}>
            <div
              className={`sticky top-0 z-10 flex items-center gap-2 border-y border-deck-line px-2.5 py-1.5 backdrop-blur ${
                isActive ? 'bg-live/8' : 'bg-deck-800/90'
              }`}
            >
              <button
                onClick={() => setCollapsed((c) => ({ ...c, [n.id]: isOpen }))}
                className="grid h-4 w-4 shrink-0 place-items-center text-ink-faint transition-colors hover:text-ink"
                aria-label={isOpen ? `Collapse ${n.name}` : `Expand ${n.name}`}
              >
                {isOpen ? <ChevronDown size={13} /> : <ChevronRight size={13} />}
              </button>

              <span
                className={`truncate text-[12.5px] font-medium ${
                  isActive ? 'text-live' : 'text-ink'
                }`}
              >
                {n.name}
              </span>

              <span className="shrink-0 font-mono text-[10px] text-ink-faint">
                {isActive ? `${members.filter((p) => p.quality !== 'Offline').length}/${members.length}` : n.deviceCount}
              </span>

              <div className="ml-auto flex shrink-0 items-center gap-0.5">
                {n.inviteCode && (
                  <IconAction label={`Invite people to ${n.name}`} onClick={() => onShare(n)}>
                    <QrCode size={12} />
                  </IconAction>
                )}
                {isActive ? (
                  <IconAction label="Disconnect" tone="fail" onClick={onDisconnect} disabled={busy}>
                    <Link2Off size={12} />
                  </IconAction>
                ) : (
                  <IconAction
                    label={connected ? 'Disconnect first' : `Connect to ${n.name}`}
                    tone="live"
                    onClick={() => onConnect(n)}
                    disabled={busy || connected}
                  >
                    <Link2 size={12} />
                  </IconAction>
                )}
              </div>
            </div>

            <AnimatePresence initial={false}>
              {isOpen && (
                <motion.div
                  initial={{ height: 0, opacity: 0 }}
                  animate={{ height: 'auto', opacity: 1 }}
                  exit={{ height: 0, opacity: 0 }}
                  transition={{ duration: 0.16, ease: 'easeOut' }}
                  className="overflow-hidden"
                >
                  {isActive && (
                    <SelfRow name={selfName} address={peersSelfAddress} onCopy={onCopy} />
                  )}
                  {members.map((peer) => (
                    <PeerRow
                      key={peer.deviceId}
                      peer={peer}
                      isExit={peer.deviceId === exitNodeId}
                      onCopy={onCopy}
                      onUseExit={onUseExit}
                      onStopExit={onStopExit}
                      onProperties={onProperties}
                    />
                  ))}
                  {isActive && members.length === 0 && (
                    <p className="px-9 py-2.5 text-[11.5px] text-ink-faint">
                      No other machines here yet.
                    </p>
                  )}
                  {!isActive && (
                    <p className="px-9 py-2.5 text-[11.5px] text-ink-faint">
                      Connect to see the machines on this network.
                    </p>
                  )}
                </motion.div>
              )}
            </AnimatePresence>
          </div>
        );
      })}
    </div>
  );
}

/** Filled in by App via the module-level setter below. */
let peersSelfAddress = '';
export function setSelfAddress(addr: string) {
  peersSelfAddress = addr;
}

/** This machine, shown in its own network the way Radmin shows you in yours. */
function SelfRow({
  name,
  address,
  onCopy,
}: {
  name: string;
  address: string;
  onCopy: (text: string, label?: string) => void;
}) {
  return (
    <div className="flex items-center gap-2.5 px-2.5 py-[5px] pl-7">
      <SignalBars latencyMs={0} online={Boolean(address)} />
      <span className="truncate text-[12.5px] text-ink-dim">
        {name} <span className="text-ink-faint">(this machine)</span>
      </span>
      <span className="ml-auto shrink-0 font-mono text-[11.5px] text-ink-dim">
        {address || '—'}
      </span>
      <button
        onClick={() => onCopy(address, 'Your address copied')}
        disabled={!address}
        title="Copy your address"
        className="grid h-5 w-5 shrink-0 place-items-center text-ink-faint transition-colors hover:text-ink disabled:opacity-30"
      >
        <Copy size={11} aria-hidden="true" />
        <span className="sr-only">Copy your address</span>
      </button>
    </div>
  );
}

function PeerRow({
  peer,
  isExit,
  onCopy,
  onUseExit,
  onStopExit,
  onProperties,
}: {
  peer: agent.Peer;
  isExit: boolean;
  onCopy: (text: string, label?: string) => void;
  onUseExit: (peer: agent.Peer) => void;
  onStopExit: () => void;
  onProperties: (peer: agent.Peer) => void;
}) {
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const online = peer.quality !== 'Offline';

  return (
    <>
      <motion.div
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        onContextMenu={(e) => {
          e.preventDefault();
          setMenu({ x: e.clientX, y: e.clientY });
        }}
        onDoubleClick={() => onProperties(peer)}
        className={`group flex cursor-default items-center gap-2.5 px-2.5 py-[5px] pl-7 transition-colors ${
          isExit ? 'bg-live/8' : 'hover:bg-deck-700/70'
        }`}
      >
        <SignalBars
          latencyMs={peer.latencyMs}
          online={online}
          relayed={peer.mode === 'relay'}
        />

        <span className={`truncate text-[12.5px] ${online ? 'text-ink' : 'text-ink-faint'}`}>
          {peer.deviceName || 'Unnamed machine'}
        </span>

        {isExit && (
          <span className="shrink-0 border border-live/40 px-1 font-mono text-[9px] tracking-[0.1em] uppercase text-live">
            exit
          </span>
        )}
        {peer.exitNode && !isExit && online && (
          <Globe size={10} className="shrink-0 text-ink-faint" aria-label="Offers to be an exit node" />
        )}

        <span
          className={`ml-auto w-[46px] shrink-0 text-right font-mono text-[10.5px] ${rtt(peer)}`}
        >
          {online && peer.latencyMs >= 0 ? `${peer.latencyMs}ms` : ''}
        </span>

        <span
          className={`w-[104px] shrink-0 text-right font-mono text-[11.5px] ${
            online ? 'text-ink-dim' : 'text-ink-faint'
          }`}
        >
          {peer.virtualIp || '—'}
        </span>
      </motion.div>

      {menu && (
        <ContextMenu
          x={menu.x}
          y={menu.y}
          onClose={() => setMenu(null)}
          items={[
            {
              label: 'Copy IP address',
              icon: Copy,
              disabled: !peer.virtualIp,
              onSelect: () => onCopy(peer.virtualIp, `${peer.deviceName || 'Address'} copied`),
            },
            // Only offered by machines that actually said they would carry
            // traffic. Showing it everywhere would invite a click that
            // fails for a reason the user cannot see.
            ...(peer.exitNode && online
              ? [
                  {
                    label: isExit ? 'Stop routing through this machine' : 'Route everything through this machine',
                    icon: Globe,
                    onSelect: () => (isExit ? onStopExit() : onUseExit(peer)),
                  },
                ]
              : []),
            { separator: true as const },
            { label: 'Properties', icon: Info, onSelect: () => onProperties(peer) },
          ]}
        />
      )}
    </>
  );
}

type MenuItem =
  | { separator: true }
  | {
      label: string;
      icon: typeof Copy;
      disabled?: boolean;
      onSelect: () => void;
      separator?: false;
    };

/**
 * A right-click menu, positioned at the pointer and clamped to the window.
 *
 * Clamping matters more than it sounds: the rows this opens from run to the
 * bottom-right of the window, which is exactly where an unclamped menu opens
 * off-screen and becomes unusable for the last few machines in the list.
 */
function ContextMenu({
  x,
  y,
  items,
  onClose,
}: {
  x: number;
  y: number;
  items: MenuItem[];
  onClose: () => void;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState({ x, y });

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    setPos({
      x: Math.min(x, window.innerWidth - r.width - 6),
      y: Math.min(y, window.innerHeight - r.height - 6),
    });
  }, [x, y]);

  useEffect(() => {
    const close = () => onClose();
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && onClose();

    // Deferred by a tick. React flushes the state update for a discrete
    // event synchronously, so an effect registered immediately would catch
    // the very contextmenu event that opened this menu and close it again
    // before it was ever painted.
    const arm = window.setTimeout(() => {
      window.addEventListener('mousedown', close);
      window.addEventListener('contextmenu', close);
      window.addEventListener('keydown', onKey);
    }, 0);

    return () => {
      window.clearTimeout(arm);
      window.removeEventListener('mousedown', close);
      window.removeEventListener('contextmenu', close);
      window.removeEventListener('keydown', onKey);
    };
  }, [onClose]);

  return (
    <motion.div
      ref={ref}
      role="menu"
      initial={{ opacity: 0, scale: 0.97 }}
      animate={{ opacity: 1, scale: 1 }}
      transition={{ duration: 0.09 }}
      onClick={(e) => e.stopPropagation()}
      className="panel fixed z-50 min-w-[210px] py-1"
      style={{ left: pos.x, top: pos.y }}
    >
      {items.map((item, i) =>
        item.separator ? (
          <div key={i} className="my-1 border-t border-deck-line" />
        ) : (
          <button
            key={i}
            role="menuitem"
            disabled={item.disabled}
            onClick={() => {
              item.onSelect();
              onClose();
            }}
            className="flex w-full items-center gap-2.5 px-3 py-1.5 text-left text-[12px] text-ink-dim transition-colors hover:bg-live/12 hover:text-ink disabled:opacity-35 disabled:hover:bg-transparent"
          >
            <item.icon size={12} className="shrink-0" aria-hidden="true" />
            {item.label}
          </button>
        ),
      )}
    </motion.div>
  );
}

function IconAction({
  label,
  children,
  onClick,
  disabled,
  tone = 'ink',
}: {
  label: string;
  children: React.ReactNode;
  onClick: () => void;
  disabled?: boolean;
  tone?: 'ink' | 'live' | 'fail';
}) {
  const hover =
    tone === 'live' ? 'hover:text-live' : tone === 'fail' ? 'hover:text-fail' : 'hover:text-ink';
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      title={label}
      className={`grid h-5 w-5 place-items-center text-ink-faint transition-colors disabled:opacity-30 disabled:hover:text-ink-faint ${hover}`}
    >
      {children}
      <span className="sr-only">{label}</span>
    </button>
  );
}

/** Colour thresholds matching the words client/agent derives. */
function rtt(peer: agent.Peer): string {
  if (peer.quality === 'Offline' || peer.latencyMs < 0) return 'text-ink-faint';
  if (peer.latencyMs < 50) return 'text-live';
  if (peer.latencyMs < 150) return 'text-ink-dim';
  return 'text-fail';
}
