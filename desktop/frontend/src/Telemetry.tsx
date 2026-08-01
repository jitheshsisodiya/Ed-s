/**
 * Live instrumentation: round-trip time and throughput, drawn as small
 * multiples rather than one chart with two axes.
 *
 * Two series with different units on shared axes is the classic way to make
 * a chart that cannot be read — the crossing point means nothing, and every
 * viewer tries to read it anyway. Separate panels each keep an honest zero
 * and their own scale.
 */

export interface Sample {
  /** Round-trip time in milliseconds; -1 when it has not been measured. */
  latencyMs: number;
  /** Bytes sent and received since the previous sample. */
  sentDelta: number;
  recvDelta: number;
}

export default function Telemetry({ history }: { history: Sample[] }) {
  const latency = history.map((s) => (s.latencyMs < 0 ? 0 : s.latencyMs));
  const sent = history.map((s) => s.sentDelta);
  const recv = history.map((s) => s.recvDelta);

  const lastLatency = history.at(-1)?.latencyMs ?? -1;

  return (
    <div className="flex flex-col gap-3">
      <Trace
        label="Round trip"
        unit="ms"
        series={latency}
        current={lastLatency < 0 ? '—' : String(lastLatency)}
        color="var(--color-live)"
      />
      <Trace
        label="Down"
        unit="/s"
        series={recv}
        current={rate(recv.at(-1) ?? 0)}
        color="var(--color-live)"
      />
      <Trace
        label="Up"
        unit="/s"
        series={sent}
        current={rate(sent.at(-1) ?? 0)}
        color="var(--color-work)"
      />
    </div>
  );
}

function Trace({
  label,
  unit,
  series,
  current,
  color,
}: {
  label: string;
  unit: string;
  series: number[];
  current: string;
  color: string;
}) {
  const w = 220;
  const h = 42;
  // A floor on the scale stops an idle tunnel from rendering sensor noise
  // as a mountain range, which is the fastest way to make a real spike
  // unreadable later.
  const peak = Math.max(...series, 1);
  const step = series.length > 1 ? w / (series.length - 1) : w;

  const points = series.map((v, i) => [i * step, h - (v / peak) * (h - 4) - 2] as const);
  const line = points.map(([x, y], i) => `${i === 0 ? 'M' : 'L'}${x.toFixed(1)} ${y.toFixed(1)}`).join(' ');
  const area = points.length
    ? `${line} L${w} ${h} L0 ${h} Z`
    : '';
  const tip = points.at(-1);

  return (
    <div>
      <div className="mb-1 flex items-baseline justify-between">
        <span className="eyebrow">{label}</span>
        <span className="font-mono text-[11px] text-ink">
          {current}
          <span className="ml-0.5 text-ink-faint">{unit}</span>
        </span>
      </div>
      <div className="grid-field relative h-[42px] w-full overflow-hidden rounded-[2px] border border-deck-line">
        <svg
          viewBox={`0 0 ${w} ${h}`}
          preserveAspectRatio="none"
          className="absolute inset-0 h-full w-full"
          aria-hidden="true"
        >
          {area && (
            <path
              d={area}
              fill={`color-mix(in oklab, ${color} 14%, transparent)`}
              stroke="none"
            />
          )}
          {line && (
            <path
              d={line}
              fill="none"
              stroke={color}
              strokeWidth={1.25}
              strokeLinejoin="round"
              vectorEffect="non-scaling-stroke"
            />
          )}
          {/* The endpoint is emphasised because it is the only value that
              is true right now; everything to its left is history. */}
          {tip && <circle cx={tip[0]} cy={tip[1]} r={2} fill={color} />}
        </svg>
      </div>
    </div>
  );
}

/** Bytes per second, at the width a column can hold. */
function rate(bytes: number): string {
  if (bytes <= 0) return '0';
  const units = ['B', 'K', 'M', 'G'];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const scaled = bytes / 1024 ** i;
  return `${scaled.toFixed(i === 0 ? 0 : 1)}${units[i]}`;
}
