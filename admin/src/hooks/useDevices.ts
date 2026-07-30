import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';
import { networksKeys } from './useNetworks';
import type { Device } from '@/lib/types';

/**
 * The API only exposes devices scoped to a network
 * (`GET /networks/{networkId}/devices`) -- there is no cross-network
 * "list all my devices" endpoint. This hook fans out across every network
 * the current user belongs to and flattens the results, annotating each
 * device with the network it came from so the UI can filter/search and
 * link back to network detail pages.
 */
export function useAllDevices() {
  const networksQuery = useQuery({
    queryKey: networksKeys.all,
    queryFn: () => api.networks.list(),
  });

  const networks = networksQuery.data ?? [];

  const devicesQuery = useQuery({
    queryKey: ['devices', 'all', networks.map((n) => n.id).sort()],
    queryFn: async (): Promise<Device[]> => {
      const results = await Promise.all(
        networks.map(async (network) => {
          const devices = await api.networks.devices(network.id);
          return devices.map((d) => ({ ...d, networkId: network.id, networkName: network.name }));
        }),
      );
      return results.flat();
    },
    enabled: networksQuery.isSuccess,
    refetchInterval: 15000,
  });

  return {
    data: devicesQuery.data,
    isLoading: networksQuery.isLoading || devicesQuery.isLoading,
    isError: networksQuery.isError || devicesQuery.isError,
    error: networksQuery.error ?? devicesQuery.error,
    refetch: devicesQuery.refetch,
  };
}

export function useDevice(deviceId: string | undefined) {
  return useQuery({
    queryKey: ['devices', deviceId],
    queryFn: () => api.devices.get(deviceId as string),
    enabled: !!deviceId,
  });
}

export function useRemoveDevice() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (deviceId: string) => api.devices.remove(deviceId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['devices'] });
      qc.invalidateQueries({ queryKey: networksKeys.all });
    },
  });
}
