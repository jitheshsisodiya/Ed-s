import { AnimatePresence, motion, useReducedMotion } from 'framer-motion';

export type Phase = 'idle' | 'handshaking' | 'active' | 'dropped';

/**
 * The one control.
 *
 * Radmin's whole interface is a list and a switch, and this is the switch.
 * Everything the brief asks a reactor core to do is in service of one
 * question a user asks from across the room — is it up — so the answer is
 * carried by colour and motion before any word is read.
 *
 * The four states are visually distinct at a glance and not merely by hue,
 * because a hue-only difference fails for the eight percent of men who
 * cannot reliably separate the red from the green: idle is still, working
 * spins, live breathes, failed pulses hard and off-rhythm.
 */
export default function ReactorCore({
  phase,
  label,
  detail,
  disabled,
  size = 92,
  onClick,
}: {
  phase: Phase;
  label: string;
  detail?: string;
  disabled?: boolean;
  /** Outer diameter. The core scales to roughly two thirds of it. */
  size?: number;
  onClick: () => void;
}) {
  const still = useReducedMotion();
  const t = TREATMENT[phase];
  const core = Math.round(size * 0.66);
  const inset = Math.round(size * 0.085);

  return (
    <div
      className="relative z-10 grid shrink-0 place-items-center"
      style={{ width: size, height: size }}
    >
      {/* Static outer track. It never moves, so the ring that does has
          something to be measured against. */}
      <div
        className="pointer-events-none absolute inset-0 rounded-full border"
        style={{ borderColor: `color-mix(in oklab, ${t.color} 22%, transparent)` }}
      />
      <div
        className="pointer-events-none absolute rounded-full border"
        style={{
          inset,
          borderColor: `color-mix(in oklab, ${t.color} 14%, transparent)`,
        }}
      />

      {/* Scanning ring: only while there is genuinely something to wait
          for. A spinner on a settled state is the oldest lie in UI. */}
      <AnimatePresence>
        {phase === 'handshaking' && !still && (
          <motion.div
            key="scan"
            className="pointer-events-none absolute inset-0 rounded-full"
            style={{
              background: `conic-gradient(from 0deg, transparent 0deg, transparent 250deg, ${t.color} 340deg, transparent 360deg)`,
              maskImage: 'radial-gradient(circle, transparent 47%, #000 48%, #000 50%, transparent 51%)',
              WebkitMaskImage:
                'radial-gradient(circle, transparent 47%, #000 48%, #000 50%, transparent 51%)',
            }}
            initial={{ opacity: 0 }}
            animate={{ opacity: 1, rotate: 360 }}
            exit={{ opacity: 0 }}
            transition={{
              rotate: { duration: 1.6, repeat: Infinity, ease: 'linear' },
              opacity: { duration: 0.25 },
            }}
          />
        )}
      </AnimatePresence>

      {/* The bloom. It breathes while live, which is the difference
          between "connected" and "connected and still working". */}
      <motion.div
        className="pointer-events-none absolute rounded-full"
        style={{ inset: inset * 2, background: t.color, filter: `blur(${Math.round(size / 5)}px)` }}
        animate={
          still
            ? { opacity: t.bloom }
            : phase === 'active'
              ? { opacity: [t.bloom * 0.6, t.bloom, t.bloom * 0.6] }
              : phase === 'dropped'
                ? { opacity: [t.bloom, t.bloom * 0.25, t.bloom] }
                : { opacity: t.bloom }
        }
        transition={
          phase === 'active'
            ? { duration: 3.4, repeat: Infinity, ease: 'easeInOut' }
            : phase === 'dropped'
              ? { duration: 0.9, repeat: Infinity, ease: 'easeInOut' }
              : { duration: 0.4 }
        }
      />

      <motion.button
        type="button"
        onClick={onClick}
        disabled={disabled}
        aria-label={`${label}. ${detail ?? ''}`}
        className="ticked relative grid place-items-center rounded-full disabled:cursor-not-allowed"
        style={{
          width: core,
          height: core,
          background: `radial-gradient(circle at 50% 30%, ${t.core} 0%, var(--color-deck-800) 72%)`,
          border: `1px solid ${t.color}`,
          boxShadow: t.shadow,
        }}
        // The snap. Connecting lands with a short overshoot and settles;
        // a linear fade would read as a value changing rather than a
        // mechanism engaging.
        animate={{ scale: 1 }}
        initial={false}
        key={phase === 'active' ? 'locked' : 'open'}
        whileHover={disabled ? undefined : { scale: 1.02 }}
        whileTap={disabled ? undefined : { scale: 0.98 }}
        transition={{ type: 'spring', stiffness: 520, damping: 17, mass: 0.7 }}
      >
        <AnimatePresence mode="wait">
          <motion.div
            key={label}
            className="flex flex-col items-center gap-0.5 px-2 text-center"
            initial={{ opacity: 0, y: 6 }}
            animate={{ opacity: 1, y: 0 }}
            exit={{ opacity: 0, y: -6 }}
            transition={{ duration: 0.18 }}
          >
            <span
              className="font-mono font-semibold tracking-[0.14em] uppercase"
              style={{ color: t.color, fontSize: Math.max(10, Math.round(size / 7)) }}
            >
              {label}
            </span>
            {detail && (
              <span className="font-mono text-[9px] tracking-[0.08em] uppercase text-ink-faint">
                {detail}
              </span>
            )}
          </motion.div>
        </AnimatePresence>
      </motion.button>
    </div>
  );
}

/**
 * One row per state. Keeping them in a table rather than in conditionals
 * scattered through the markup is what makes it possible to check at a
 * glance that all four are actually distinct.
 */
const TREATMENT: Record<
  Phase,
  { color: string; core: string; shadow: string; bloom: number }
> = {
  idle: {
    color: 'var(--color-rest)',
    core: 'var(--color-deck-600)',
    shadow: '0 0 0 1px color-mix(in oklab, var(--color-rest) 30%, transparent)',
    bloom: 0.1,
  },
  handshaking: {
    color: 'var(--color-work)',
    core: 'var(--color-work-deep)',
    shadow: 'var(--shadow-glow-work)',
    bloom: 0.3,
  },
  active: {
    color: 'var(--color-live)',
    core: 'var(--color-live-deep)',
    shadow: 'var(--shadow-glow-live)',
    bloom: 0.42,
  },
  dropped: {
    color: 'var(--color-fail)',
    core: 'var(--color-fail-deep)',
    shadow: 'var(--shadow-glow-fail)',
    bloom: 0.44,
  },
};
