import { useQuery } from '@tanstack/react-query';
import { api } from '@/lib/api';

/** Auto-refreshes every 10s so the dashboard cards stay live. */
export function useDashboardStats() {
  return useQuery({
    queryKey: ['dashboard', 'stats'],
    queryFn: () => api.dashboard.stats(),
    refetchInterval: 10000,
  });
}
