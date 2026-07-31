import { useState } from 'react';
import { useNetworks } from '@/hooks/useNetworks';
import { useAuditLogs } from '@/hooks/useLogs';
import { DataTable, type DataTableColumn } from '@/components/DataTable';
import { ErrorBanner } from '@/components/Feedback';
import { formatDateTime } from '@/lib/format';
import type { AuditLog } from '@/lib/types';

export default function AuditLogsPage() {
  const { data: networks } = useNetworks();
  const [networkId, setNetworkId] = useState<string>('');
  const { data, isLoading, isError, error, refetch } = useAuditLogs(networkId || undefined);

  const columns: DataTableColumn<AuditLog>[] = [
    {
      key: 'createdAt',
      header: 'Time',
      sortable: true,
      accessor: (l) => l.createdAt,
      render: (l) => formatDateTime(l.createdAt),
    },
    {
      key: 'action',
      header: 'Action',
      sortable: true,
      accessor: (l) => l.action,
      render: (l) => <span className="font-mono text-xs">{l.action}</span>,
    },
    {
      key: 'target',
      header: 'Target',
      render: (l) =>
        l.targetType ? (
          <span>
            {l.targetType}
            {l.targetId ? <span className="text-gray-400 dark:text-gray-500"> #{l.targetId.slice(0, 8)}</span> : null}
          </span>
        ) : (
          '—'
        ),
    },
    {
      key: 'actor',
      header: 'Actor',
      render: (l) => <span className="font-mono text-xs">{l.actorUserId.slice(0, 8)}</span>,
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
      </div>

      {isError && (
        <ErrorBanner
          message={error instanceof Error ? error.message : 'Failed to load audit logs.'}
          onRetry={() => refetch()}
        />
      )}

      {!isError && (
        <DataTable
          columns={columns}
          data={data ?? []}
          keyField={(l) => l.id}
          isLoading={isLoading}
          pageSize={20}
          emptyMessage="No audit log entries found."
        />
      )}
    </div>
  );
}
