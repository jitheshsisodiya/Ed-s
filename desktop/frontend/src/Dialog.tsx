import { useEffect, useRef, type ReactNode } from 'react';
import { motion } from 'framer-motion';
import { X } from 'lucide-react';

/**
 * A modal, in the shape every desktop app uses: title bar with a close
 * affordance, body, and a footer whose actions sit bottom-right with the
 * confirming one last.
 *
 * That ordering is not decoration. On Windows the rightmost button is the
 * default and the one muscle memory reaches for, so putting Cancel there
 * makes people cancel things they meant to do.
 */
export default function Dialog({
  title,
  children,
  footer,
  onClose,
  width = 420,
}: {
  title: string;
  children: ReactNode;
  footer?: ReactNode;
  onClose: () => void;
  width?: number;
}) {
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', onKey);
    // Focus lands inside on open, so the keyboard works without a click
    // first and Escape has something to close.
    ref.current?.querySelector<HTMLElement>('input, button')?.focus();
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  return (
    <div
      className="fixed inset-0 z-40 grid place-items-center bg-deck-900/70 p-6 backdrop-blur-[2px]"
      onMouseDown={(e) => e.target === e.currentTarget && onClose()}
    >
      <motion.div
        ref={ref}
        role="dialog"
        aria-modal="true"
        aria-label={title}
        initial={{ opacity: 0, scale: 0.98, y: 6 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        transition={{ duration: 0.14, ease: 'easeOut' }}
        className="panel ticked flex max-h-full flex-col overflow-hidden"
        style={{ width }}
      >
        <div className="flex shrink-0 items-center border-b border-deck-line px-3.5 py-2">
          <span className="font-mono text-[11px] tracking-[0.16em] uppercase text-ink">
            {title}
          </span>
          <button
            onClick={onClose}
            aria-label="Close"
            className="ml-auto grid h-5 w-5 place-items-center text-ink-faint transition-colors hover:text-ink"
          >
            <X size={13} />
          </button>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto">{children}</div>

        {footer && (
          <div className="flex shrink-0 items-center justify-end gap-2 border-t border-deck-line px-3.5 py-2.5">
            {footer}
          </div>
        )}
      </motion.div>
    </div>
  );
}

/** A dialog's confirming action. */
export function Primary({
  children,
  ...rest
}: React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      {...rest}
      className="border border-live/50 bg-live/12 px-3.5 py-1 font-mono text-[10.5px] tracking-[0.14em] uppercase text-live transition-colors hover:bg-live/22 disabled:opacity-40"
    >
      {children}
    </button>
  );
}

/** A dialog's dismissing action. */
export function Secondary({
  children,
  ...rest
}: React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      {...rest}
      className="border border-deck-line-bright px-3.5 py-1 font-mono text-[10.5px] tracking-[0.14em] uppercase text-ink-dim transition-colors hover:border-ink-faint hover:text-ink disabled:opacity-40"
    >
      {children}
    </button>
  );
}

/** A destructive action, which never sits where the default button goes. */
export function Danger({
  children,
  ...rest
}: React.ButtonHTMLAttributes<HTMLButtonElement>) {
  return (
    <button
      {...rest}
      className="border border-fail/45 px-3.5 py-1 font-mono text-[10.5px] tracking-[0.14em] uppercase text-fail transition-colors hover:bg-fail/12 disabled:opacity-40"
    >
      {children}
    </button>
  );
}

/** A labelled row in a dialog form. */
export function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="mb-3 grid grid-cols-[104px_1fr] items-center gap-3 last:mb-0">
      <span className="text-right text-[12px] text-ink-dim">{label}</span>
      {children}
    </label>
  );
}

/** The text input used throughout. */
export function Input(props: React.InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      {...props}
      className="min-w-0 rounded-[2px] border border-deck-line bg-deck-900 px-2 py-1.5 font-mono text-[12px] text-ink transition-colors placeholder:text-ink-faint focus:border-live focus:outline-none"
    />
  );
}

/** An on/off switch, labelled by its state rather than only its position. */
export function Toggle({
  checked,
  onChange,
  label,
}: {
  checked: boolean;
  onChange: (v: boolean) => void;
  label: string;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      onClick={() => onChange(!checked)}
      className="flex items-center gap-2.5"
    >
      <span
        className="relative h-[18px] w-[34px] rounded-full border transition-colors"
        style={{
          borderColor: checked ? 'var(--color-live)' : 'var(--color-deck-line-bright)',
          background: checked ? 'color-mix(in oklab, var(--color-live) 22%, transparent)' : 'transparent',
        }}
      >
        <motion.span
          className="absolute top-[2px] h-[12px] w-[12px] rounded-full"
          style={{ background: checked ? 'var(--color-live)' : 'var(--color-ink-faint)' }}
          animate={{ left: checked ? 18 : 3 }}
          transition={{ type: 'spring', stiffness: 600, damping: 32 }}
        />
      </span>
      <span className="font-mono text-[10.5px] tracking-[0.12em] uppercase text-ink-dim">
        {label}
      </span>
    </button>
  );
}
