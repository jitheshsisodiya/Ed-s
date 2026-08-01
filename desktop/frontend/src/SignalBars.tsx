import { X } from 'lucide-react';

/**
 * Four bars, the way Radmin shows reachability.
 *
 * This is a better indicator than a number for the thing it answers, which
 * is "can I use this machine right now" — the shape is readable at a glance
 * and from the corner of your eye, and it degrades gracefully for anyone who
 * cannot separate the green from the grey, because the height carries the
 * same information as the colour.
 *
 * The number is still there, in its own column, for anyone who wants it.
 * The bars are the summary, not a replacement.
 */
export default function SignalBars({
  latencyMs,
  online,
  relayed,
}: {
  latencyMs: number;
  online: boolean;
  /** A relayed path is capped at three bars however fast it measures: it
      costs an extra hop, and a full-strength icon would promise otherwise. */
  relayed?: boolean;
}) {
  const strength = online ? bars(latencyMs, relayed) : 0;
  const color = online
    ? relayed
      ? 'var(--color-work)'
      : 'var(--color-live)'
    : 'var(--color-ink-faint)';

  return (
    <span className="relative inline-flex h-[13px] w-[16px] shrink-0 items-end gap-[1.5px]">
      {!online && (
        <X
          size={9}
          className="absolute -left-[7px] bottom-0 text-ink-faint"
          aria-hidden="true"
        />
      )}
      {[0, 1, 2, 3].map((i) => (
        <span
          key={i}
          className="w-[2.5px] rounded-[0.5px]"
          style={{
            height: `${4 + i * 3}px`,
            background: i < strength ? color : 'var(--color-deck-line-bright)',
          }}
        />
      ))}
    </span>
  );
}

/**
 * How many bars a measurement earns.
 *
 * The thresholds are the same ones client/agent uses to pick the word
 * "Excellent" or "Good", so four bars and that word can never disagree.
 * An unmeasured but established path gets three: reporting one would say
 * "barely reachable" about a link that is working fine.
 */
function bars(latencyMs: number, relayed?: boolean): number {
  if (relayed) return latencyMs >= 0 && latencyMs < 150 ? 3 : 2;
  if (latencyMs < 0) return 3;
  if (latencyMs < 50) return 4;
  if (latencyMs < 150) return 3;
  if (latencyMs < 300) return 2;
  return 1;
}
