import { useEffect, useRef, useState } from 'react';
import { useReducedMotion } from 'framer-motion';

/**
 * Text that churns and then locks.
 *
 * The effect earns its place because it marks a moment that genuinely
 * happens: before the tunnel is up this machine has no address on the
 * network, and the instant it does, it has one. The churn is the interval
 * where the answer is unknown, and the lock is the answer arriving.
 *
 * It settles character by character from the left, so the address resolves
 * the way an address is read rather than snapping in all at once. Anyone
 * who has asked their system not to animate gets the value directly.
 */
export default function Scrambler({
  value,
  active,
  className,
  style,
}: {
  /** The final text. Empty means there is nothing to lock onto yet. */
  value: string;
  /** Whether to run the churn at all. */
  active: boolean;
  className?: string;
  style?: React.CSSProperties;
}) {
  const still = useReducedMotion();
  const [shown, setShown] = useState(value);
  const frame = useRef<number>(0);

  useEffect(() => {
    if (!active || still || !value) {
      setShown(value);
      return;
    }

    // Two frames per character: fast enough to read as churn, slow
    // enough that the digits are individually visible rather than a grey
    // blur, which is what makes it look like a readout and not a glitch.
    const total = value.length * 2;
    let tick = 0;
    let timer = 0;

    const step = () => {
      tick += 1;
      const settled = Math.floor(tick / 2);
      setShown(
        value
          .split('')
          .map((ch, i) => {
            if (i < settled) return ch;
            // Structure is never scrambled. Dots stay dots, so the shape
            // of an address is legible throughout and the width never
            // shifts.
            if (ch === '.' || ch === ':' || ch === '/') return ch;
            return DIGITS[Math.floor(Math.random() * DIGITS.length)];
          })
          .join(''),
      );
      if (tick < total) timer = window.setTimeout(step, 45);
      else setShown(value);
    };

    frame.current = window.setTimeout(step, 0);
    return () => {
      window.clearTimeout(frame.current);
      window.clearTimeout(timer);
    };
  }, [value, active, still]);

  return (
    <span className={className} style={style} aria-label={value || undefined}>
      {shown || '—'}
    </span>
  );
}

const DIGITS = '0123456789';
