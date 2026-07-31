import { useMemo, useState } from 'react';
import { useNetworks } from '@/hooks/useNetworks';
import { useConnectionLogs } from '@/hooks/useLogs';
import { DataTable, type DataTableColumn } from '@/components/DataTable';
import { ErrorBanner } from '@/components/Feedback';
import { formatDateTime } from '@/lib/format';
import type { ConnectionLog } from '@/lib/types';

export default function ConnectionLogsPage() {
  const { data: networks } = useNetworks();
  const [networkId, setNetworkId] = useState<string>('');
  const [eventType, setEventType] = useState<string>('');
  const { data, isLoading, isError, error, refetch } = useConnectionLogs(networkId || undefined);

  const eventTypes = useMemo(() => {
    const set = new Set<string>();
    (data ?? []).forEach((l) => set.add(l.eventType));
    return Array.from(set).sort();
  }, [data]);

  const filtered = useMemo(() => {
    if (!eventType) return data ?? [];
    return (data ?? []).filter((l) => l.eventType === eventType);
  }, [data, eventType]);

  const columns: DataTableColumn<ConnectionLog>[] = [
    {
      key: 'createdAt',
      header: 'Time',
      sortable: true,
      accessor: (l) => l.createdAt,
      render: (l) => formatDateTime(l.createdAt),
    },
    {
      key: 'eventType',
      header: 'Event',
      sortable: true,
      accessor: (l) => l.eventType,
      render: (l) => (
        <span className="inline-flex items-center rounded-full bg-gray-100 px-2 py-0.5 text-xs font-medium text-gray-700 dark:bg-gray-800 dark:text-gray-200">
          {l.eventType}
        </span>
      ),
    },
    {
      key: 'device',
      header: 'Device',
      render: (l) => <span className="font-mono text-xs">{l.deviceId.slice(0, 8)}</span>,
    },
    {
      key: 'peer',
      header: 'Peer device',
      render: (l) => <span className="font-mono text-xs">{l.peerDeviceId ? l.peerDeviceId.slice(0, 8) : '—'}</span>,
    },
    {
      key: 'latency',
      header: 'Latency',
      sortable: true,
      accessor: (l) => l.latencyMs ?? Number.MAX_SAFE_INTEGER,
      render: (l) => (typeof l.latencyMs === 'number' ? `${l.latencyMs} ms` : '—'),
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-end gap-3">
        <div className="flex flex-col gap-1">
          <label htmlFor="network-filter" className="text-sm font-medium text-gray-700 dark:text-gray-300">
            Network
          </label>
          <select
            id="network-filter"
            value={networkId}
            onChange={(e) => setNetworkId(e.target.value)}
            className="rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-900 dark:text-gray-100"
          >
            <option value="">All networks</option>
            {(networks ?? []).map((n) => (
              <option key={n.id} value={n.id}>
                {n.name}
              </option>
            ))}
          </select>
        </div>
        <div className="flex flex-col gap-1">
          <label htmlFor="event-filter" className="text-sm font-medium text-gray-700 dark:text-gray-300">
            Event type
          </label>
          <select
            id="event-filter"
            value={eventType}
            onChange={(e) => setEventType(e.target.value)}
            className="rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-900 dark:text-gray-100"
          >
            <option value="">All events</option>
            {eventTypes.map((t) => (
              <option key={t} value={t}>
                {t}
              </option>
            ))}
          </select>
        </div>
      </div>

      {isError && (
        <ErrorBanner
          message={error instanceof Error ? error.message : 'Failed to load connection logs.'}
          onRetry={() => refetch()}
        />
      )}

      {!isError && (
        <DataTable
          columns={columns}
          data={filtered}
          keyField={(l) => l.id}
          isLoading={isLoading}
          pageSize={20}
          emptyMessage="No connection log entries found."
        />
      )}
    </div>
  );
}
