import type { DeviceStatus } from '@/lib/types';

const config: Record<DeviceStatus, { label: string; dot: string; text: string }> = {
  online: {
    label: 'Online',
    dot: 'bg-emerald-500',
    text: 'text-emerald-700 bg-emerald-50 dark:text-emerald-300 dark:bg-emerald-950',
  },
  offline: {
    label: 'Offline',
    dot: 'bg-gray-400',
    text: 'text-gray-600 bg-gray-100 dark:text-gray-300 dark:bg-gray-800',
  },
  unknown: {
    label: 'Unknown',
    dot: 'bg-amber-500',
    text: 'text-amber-700 bg-amber-50 dark:text-amber-300 dark:bg-amber-950',
  },
};

export function StatusBadge({ status }: { status: DeviceStatus }) {
  const c = config[status] ?? config.unknown;
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium ${c.text}`}
    >
      <span className={`h-1.5 w-1.5 rounded-full ${c.dot}`} aria-hidden="true" />
      {c.label}
    </span>
  );
}
