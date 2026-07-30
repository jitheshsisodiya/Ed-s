import { useEffect, useRef, useState } from 'react';
import {
  Area,
  AreaChart,
  CartesianGrid,
  Cell,
  Legend,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';
import { useDashboardStats } from '@/hooks/useDashboard';
import { Card, ErrorBanner, Spinner } from '@/components/Feedback';
import { formatBytes, formatPercent } from '@/lib/format';

interface HistoryPoint {
  time: string;
  onlineDevices: number;
  relayBandwidthBytes: number;
}

const MAX_HISTORY_POINTS = 20;

function StatCard({
  label,
  value,
  sublabel,
}: {
  label: string;
  value: string;
  sublabel?: string;
}) {
  return (
    <Card>
      <p className="text-sm font-medium text-gray-500 dark:text-gray-400">{label}</p>
      <p className="mt-2 text-3xl font-semibold tabular-nums text-gray-900 dark:text-gray-100">{value}</p>
      {sublabel && <p className="mt-1 text-xs text-gray-400 dark:text-gray-500">{sublabel}</p>}
    </Card>
  );
}

export default function DashboardPage() {
  const { data, isLoading, isError, error, refetch, dataUpdatedAt } = useDashboardStats();
  const [history, setHistory] = useState<HistoryPoint[]>([]);
  const lastRecordedAt = useRef<number>(0);

  useEffect(() => {
    if (!data || dataUpdatedAt === lastRecordedAt.current) return;
    lastRecordedAt.current = dataUpdatedAt;
    setHistory((prev) => {
      const next = [
        ...prev,
        {
          time: new Date(dataUpdatedAt).toLocaleTimeString(undefined, {
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit',
          }),
          onlineDevices: data.onlineDevices,
          relayBandwidthBytes: data.relayBandwidthBytes,
        },
      ];
      return next.slice(-MAX_HISTORY_POINTS);
    });
  }, [data, dataUpdatedAt]);

  if (isLoading) return <Spinner label="Loading dashboard…" />;

  if (isError || !data) {
    return (
      <ErrorBanner
        message={error instanceof Error ? error.message : 'Failed to load dashboard stats.'}
        onRetry={() => refetch()}
      />
    );
  }

  const connectivityData = [
    { name: 'Direct (P2P)', value: data.p2pConnectionRatio },
    { name: 'Relayed', value: Math.max(0, 1 - data.p2pConnectionRatio) },
  ];
  const pieColors = ['#4a5cf7', '#c4d4ff'];

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <p className="text-sm text-gray-500 dark:text-gray-400">
          Auto-refreshes every 10 seconds. Last updated{' '}
          {new Date(dataUpdatedAt).toLocaleTimeString()}.
        </p>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-5">
        <StatCard label="Active users" value={data.activeUsers.toLocaleString()} />
        <StatCard label="Active networks" value={data.activeNetworks.toLocaleString()} />
        <StatCard
          label="Online devices"
          value={`${data.onlineDevices.toLocaleString()} / ${data.totalDevices.toLocaleString()}`}
          sublabel="online / total"
        />
        <StatCard label="Relay bandwidth" value={formatBytes(data.relayBandwidthBytes)} />
        <StatCard label="P2P connection ratio" value={formatPercent(data.p2pConnectionRatio)} />
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <Card className="lg:col-span-2">
          <h2 className="mb-4 text-sm font-semibold text-gray-700 dark:text-gray-200">
            Online devices &amp; relay bandwidth over this session
          </h2>
          {history.length < 2 ? (
            <p className="py-16 text-center text-sm text-gray-400 dark:text-gray-500">
              Collecting data points as the dashboard refreshes…
            </p>
          ) : (
            <ResponsiveContainer width="100%" height={260}>
              <AreaChart data={history} margin={{ top: 5, right: 10, left: -10, bottom: 0 }}>
                <defs>
                  <linearGradient id="onlineGradient" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#4a5cf7" stopOpacity={0.4} />
                    <stop offset="95%" stopColor="#4a5cf7" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" className="stroke-gray-200 dark:stroke-gray-800" />
                <XAxis dataKey="time" tick={{ fontSize: 11 }} minTickGap={20} />
                <YAxis tick={{ fontSize: 11 }} allowDecimals={false} />
                <Tooltip
                  formatter={(value: number, name) =>
                    name === 'onlineDevices' ? [value, 'Online devices'] : [formatBytes(value), 'Relay bandwidth']
                  }
                  contentStyle={{ fontSize: 12, borderRadius: 8 }}
                />
                <Area
                  type="monotone"
                  dataKey="onlineDevices"
                  stroke="#4a5cf7"
                  fill="url(#onlineGradient)"
                  strokeWidth={2}
                />
              </AreaChart>
            </ResponsiveContainer>
          )}
        </Card>

        <Card>
          <h2 className="mb-4 text-sm font-semibold text-gray-700 dark:text-gray-200">
            Connectivity mix
          </h2>
          <ResponsiveContainer width="100%" height={220}>
            <PieChart>
              <Pie
                data={connectivityData}
                dataKey="value"
                nameKey="name"
                innerRadius={50}
                outerRadius={80}
                paddingAngle={2}
              >
                {connectivityData.map((entry, i) => (
                  <Cell key={entry.name} fill={pieColors[i % pieColors.length]} />
                ))}
              </Pie>
              <Tooltip formatter={(value: number) => formatPercent(value)} contentStyle={{ fontSize: 12, borderRadius: 8 }} />
              <Legend wrapperStyle={{ fontSize: 12 }} />
            </PieChart>
          </ResponsiveContainer>
        </Card>
      </div>
    </div>
  );
}
