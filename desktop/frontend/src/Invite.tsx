import { useEffect, useState } from 'react';
import qr from 'qrcode-generator';

import { InviteLink } from '../wailsjs/go/main/App';
import type { agent } from '../wailsjs/go/models';

/**
 * How somebody else gets in.
 *
 * A network nobody can join is a network of one, so this is the other half
 * of "create a network" and it is one click from the network row rather than
 * buried in a management screen. The code is shown large enough to read
 * aloud over a call, the link is one click to the clipboard, and the QR is
 * there so a phone joins by pointing its camera at the screen — no typing,
 * no retyping a character somebody misheard.
 */
export default function Invite({
  network,
  onClose,
  onCopy,
}: {
  network: agent.Network;
  onClose: () => void;
  onCopy: (text: string, label?: string) => void;
}) {
  const [link, setLink] = useState('');

  useEffect(() => {
    InviteLink(network.inviteCode).then(setLink).catch(() => undefined);
  }, [network.inviteCode]);

  // Close on Escape: this is a sheet over the app, and every sheet on every
  // desktop closes that way.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  return (
    <div className="scrim" onClick={onClose} role="presentation">
      <div
        className="sheet-card fade-in"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label={`Invite people to ${network.name}`}
      >
        <div className="section-head">
          <h2>Invite</h2>
          <button className="text-btn" onClick={onClose}>
            Done
          </button>
        </div>

        <p className="hint" style={{ marginTop: 0 }}>
          Anyone with this code can join <strong>{network.name}</strong>. It keeps working
          until you replace it.
        </p>

        <div className="invite">
          {link && <QrCode value={link} />}
          <div className="invite-code" aria-label="Invite code">
            {network.inviteCode}
          </div>
          <div className="invite-actions">
            <button
              className="btn primary"
              onClick={() => onCopy(network.inviteCode, 'Invite code copied')}
            >
              Copy code
            </button>
            <button
              className="btn"
              disabled={!link}
              onClick={() => onCopy(link, 'Invite link copied')}
            >
              Copy link
            </button>
          </div>
          <p className="hint" style={{ textAlign: 'center' }}>
            On a phone, open NexusVPN and scan this.
          </p>
        </div>
      </div>
    </div>
  );
}

/**
 * Renders a QR code as a single SVG path.
 *
 * One path rather than a grid of rects: a 25×25 symbol is hundreds of
 * elements otherwise, and this has to repaint whenever the sheet opens.
 */
function QrCode({ value, size = 168 }: { value: string; size?: number }) {
  const code = qr(0, 'M');
  code.addData(value);
  code.make();

  const count = code.getModuleCount();
  const quiet = 2;
  const span = count + quiet * 2;

  let d = '';
  for (let row = 0; row < count; row++) {
    for (let col = 0; col < count; col++) {
      if (code.isDark(row, col)) {
        d += `M${col + quiet} ${row + quiet}h1v1h-1z`;
      }
    }
  }

  return (
    <svg
      className="qr"
      width={size}
      height={size}
      viewBox={`0 0 ${span} ${span}`}
      shapeRendering="crispEdges"
      role="img"
      aria-label="QR code containing the invite link"
    >
      <rect width={span} height={span} fill="#fff" />
      <path d={d} fill="#000" />
    </svg>
  );
}
