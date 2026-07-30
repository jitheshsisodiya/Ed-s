import { useQuery } from '@tanstack/react-query';
import { api } from '@/lib/api';

export function useAuditLogs(networkId: string | undefined) {
  return useQuery({
    queryKey: ['logs', 'audit', networkId ?? 'all'],
    queryFn: () => api.logs.audit(networkId),
  });
}

export function useConnectionLogs(networkId: string | undefined) {
  return useQuery({
    queryKey: ['logs', 'connections', networkId ?? 'all'],
    queryFn: () => api.logs.connections(networkId),
  });
}
