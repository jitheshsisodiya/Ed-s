import { useCallback, useEffect, useRef, useState } from 'react';
import qr from 'qrcode-generator';

import { CopyToClipboard, StartPairing } from '../wailsjs/go/main/App';
import type { agent } from '../wailsjs/go/models';

import Dialog, { Primary, Secondary } from './Dialog';

/**
 * Adding another device — a phone, or a second computer.
 *
 * The alternative is typing a server address, an email and a password on the
 * other device, and the address is the part nobody can be expected to know —
 * it is whatever private address the router handed this machine. So the code
 * carries the address with it, and the other device is told nothing at all.
 *
 * A phone scans it. A computer has no camera pointed at this screen, so it
 * gets the same code as text to paste; the QR is a delivery mechanism, not the
 * security. What is being trusted either way is a code that works once, dies
 * in minutes, and names the certificate the server has to present.
 *
 * Both of those facts are shown rather than assumed, because a code that has
 * quietly gone stale looks exactly like one that does not work.
 */
export default function PairPhone({
  network,
  onClose,
}: {
  network: agent.Network;
  onClose: () => void;
}) {
  const [link, setLink] = useState<agent.PairingLink | null>(null);
  const [error, setError] = useState('');
  const [remaining, setRemaining] = useState(0);
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState(false);

  const issue = useCallback(() => {
    setBusy(true);
    setError('');
    // A new code makes the old one worthless, so the button must stop
    // claiming the clipboard holds anything useful.
    setCopied(false);
    StartPairing(network.id)
      .then((l) => {
        setLink(l);
        setRemaining(l.expiresIn);
      })
      .catch((e) => setError(String(e)))
      .finally(() => setBusy(false));
  }, [network.id]);

  useEffect(issue, [issue]);

  // Counts down rather than expiring silently: a code that has run out looks
  // identical to one that never worked, and the difference matters to
  // somebody standing there with a phone.
  useEffect(() => {
    if (remaining <= 0) return;
    const id = window.setInterval(() => setRemaining((r) => Math.max(0, r - 1)), 1000);
    return () => window.clearInterval(id);
  }, [remaining > 0]);

  const expired = link !== null && remaining <= 0;

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  return (
    <Dialog
      title="Add a device"
      onClose={onClose}
      width={420}
      footer={
        <>
          <Secondary onClick={issue} disabled={busy}>
            New code
          </Secondary>
          <Primary onClick={onClose}>Done</Primary>
        </>
      }
    >
      <div className="flex flex-col items-center p-4">
        <p className="mb-3 text-center text-[12px] leading-relaxed text-ink-dim">
          On a phone, tap <strong className="text-ink">Scan</strong>. On another computer,
          copy the link below and paste it there. Either way it joins{' '}
          <strong className="text-ink">{network.name}</strong> as you — no address, no
          password to type.
        </p>

        <QRPanel value={link?.url ?? ''} dimmed={expired || busy} />

        {error ? (
          <p className="mt-3 text-center text-[12px] leading-relaxed text-fail">{error}</p>
        ) : expired ? (
          <p className="mt-3 text-center text-[12px] leading-relaxed text-work">
            This code has expired. Take a new one.
          </p>
        ) : (
          <p className="mt-3 text-center text-[11.5px] leading-relaxed text-ink-faint">
            Works once, and only for the next {formatRemaining(remaining)}. Anyone who scans it
            is signed in as you, so do not leave it on screen.
          </p>
        )}

        {link?.url && !expired && (
          <button
            type="button"
            onClick={() => {
              void CopyToClipboard(link.url).then(
                () => setCopied(true),
                () => setError('Could not reach the clipboard.'),
              );
            }}
            className="mt-3 border border-deck-line px-3 py-1.5 font-mono text-[10px] tracking-[0.14em] uppercase text-ink-dim transition-colors hover:border-live/50 hover:text-live"
          >
            {copied ? 'link copied' : 'copy link for a computer'}
          </button>
        )}

        {link?.serverUrl && (
          <p className="mt-2 text-center font-mono text-[10.5px] text-ink-faint">
            {link.serverUrl}
          </p>
        )}
      </div>
    </Dialog>
  );
}

function formatRemaining(seconds: number) {
  if (seconds <= 0) return 'no time';
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  if (m === 0) return `${s} second${s === 1 ? '' : 's'}`;
  return `${m}:${String(s).padStart(2, '0')}`;
}

/** Renders the code, or a placeholder of the same size so nothing jumps. */
function QRPanel({ value, dimmed }: { value: string; dimmed: boolean }) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const host = ref.current;
    if (!host) return;
    host.innerHTML = '';
    if (!value) return;

    // Error-correction level L, because this payload is long — a server
    // address and a signed token — and a higher level would push the code
    // into a denser version that a phone camera reads less reliably at
    // arm's length from a monitor.
    const code = qr(0, 'L');
    code.addData(value);
    code.make();
    host.innerHTML = code.createSvgTag({ cellSize: 4, margin: 2, scalable: true });
    const svg = host.querySelector('svg');
    if (svg) {
      svg.setAttribute('width', '100%');
      svg.setAttribute('height', '100%');
      svg.style.display = 'block';
    }
  }, [value]);

  return (
    <div
      className="grid h-[196px] w-[196px] place-items-center rounded-[3px] bg-white p-1.5 transition-opacity"
      style={{ opacity: dimmed ? 0.25 : 1 }}
      aria-label={value ? 'Pairing code' : 'Preparing a pairing code'}
    >
      <div ref={ref} className="h-full w-full [&_svg]:h-full [&_svg]:w-full" />
    </div>
  );
}
