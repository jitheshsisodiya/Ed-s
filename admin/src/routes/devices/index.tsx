import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { useAllDevices } from '@/hooks/useDevices';
import { DataTable, type DataTableColumn } from '@/components/DataTable';
import { ErrorBanner } from '@/components/Feedback';
import { StatusBadge } from '@/components/StatusBadge';
import { Input } from '@/components/Input';
import { formatBytes, formatRelativeTime } from '@/lib/format';
import type { Device, DeviceStatus } from '@/lib/types';

const STATUS_FILTERS: { key: DeviceStatus | 'all'; label: string }[] = [
  { key: 'all', label: 'All' },
  { key: 'online', label: 'Online' },
  { key: 'offline', label: 'Offline' },
  { key: 'unknown', label: 'Unknown' },
];

export default function DevicesIndexPage() {
  const { data, isLoading, isError, error, refetch } = useAllDevices();
  const [search, setSearch] = useState('');
  const [statusFilter, setStatusFilter] = useState<DeviceStatus | 'all'>('all');

  const filtered = useMemo(() => {
    const list = data ?? [];
    const query = search.trim().toLowerCase();
    return list.filter((d) => {
      if (statusFilter !== 'all' && d.status !== statusFilter) return false;
      if (!query) return true;
      return (
        d.name.toLowerCase().includes(query) ||
        d.os.toLowerCase().includes(query) ||
        d.virtualIp.toLowerCase().includes(query) ||
        (d.networkName ?? '').toLowerCase().includes(query) ||
        (d.lastPublicIp ?? '').toLowerCase().includes(query)
      );
    });
  }, [data, search, statusFilter]);

  const columns: DataTableColumn<Device>[] = [
    {
      key: 'name',
      header: 'Device',
      sortable: true,
      accessor: (d) => d.name,
      render: (d) => (
        <div>
          <p className="font-medium text-gray-900 dark:text-gray-100">{d.name}</p>
          <p className="text-xs text-gray-500 dark:text-gray-400">
            {d.os}
            {d.osVersion ? ` ${d.osVersion}` : ''}
          </p>
        </div>
      ),
    },
    {
      key: 'network',
      header: 'Network',
      sortable: true,
      accessor: (d) => d.networkName ?? '',
      render: (d) =>
        d.networkId ? (
          <Link to={`/networks/${d.networkId}?tab=devices`} className="text-brand-600 hover:underline dark:text-brand-400">
            {d.networkName}
          </Link>
        ) : (
          '—'
        ),
    },
    {
      key: 'status',
      header: 'Status',
      sortable: true,
      accessor: (d) => d.status,
      render: (d) => <StatusBadge status={d.status} />,
    },
    {
      key: 'latency',
      header: 'Ping',
      sortable: true,
      accessor: (d) => d.latencyMs ?? Number.MAX_SAFE_INTEGER,
      render: (d) => (typeof d.latencyMs === 'number' ? `${d.latencyMs} ms` : '—'),
    },
    {
      key: 'ip',
      header: 'Virtual IP / Public IP',
      render: (d) => (
        <div className="font-mono text-xs">
          <p>{d.virtualIp}</p>
          <p className="text-gray-400 dark:text-gray-500">{d.lastPublicIp ?? '—'}</p>
        </div>
      ),
    },
    {
      key: 'bandwidth',
      header: 'Bandwidth (sent / recv)',
      render: (d) => `${formatBytes(d.bytesSent)} / ${formatBytes(d.bytesReceived)}`,
    },
    {
      key: 'lastSeen',
      header: 'Last seen',
      sortable: true,
      accessor: (d) => d.lastSeenAt,
      render: (d) => formatRelativeTime(d.lastSeenAt),
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-gray-500 dark:text-gray-400">
          All devices across every network you belong to.
        </p>
      </div>

      <div className="flex flex-wrap items-end gap-3">
        <div className="w-full max-w-xs">
          <Input
            label="Search"
            placeholder="Name, OS, IP, network…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
        </div>
        <div className="flex gap-1 pb-0.5" role="group" aria-label="Filter by status">
          {STATUS_FILTERS.map((f) => (
            <button
              key={f.key}
              type="button"
              onClick={() => setStatusFilter(f.key)}
              aria-pressed={statusFilter === f.key}
              className={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${
                statusFilter === f.key
                  ? 'bg-brand-600 text-white'
                  : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700'
              }`}
            >
              {f.label}
            </button>
          ))}
        </div>
      </div>

      {isError && (
        <ErrorBanner
          message={error instanceof Error ? error.message : 'Failed to load devices.'}
          onRetry={() => refetch()}
        />
      )}

      {!isError && (
        <DataTable
          columns={columns}
          data={filtered}
          keyField={(d) => d.id}
          isLoading={isLoading}
          pageSize={15}
          emptyMessage={
            search || statusFilter !== 'all'
              ? 'No devices match your filters.'
              : 'No devices found. Join or create a network to see devices here.'
          }
        />
      )}
    </div>
  );
}
