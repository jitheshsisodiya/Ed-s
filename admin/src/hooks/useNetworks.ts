import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { api } from '@/lib/api';
import type { JoinNetworkRequest, NetworkCreateRequest, NetworkRole } from '@/lib/types';

export const networksKeys = {
  all: ['networks'] as const,
  detail: (id: string) => ['networks', id] as const,
  members: (id: string) => ['networks', id, 'members'] as const,
  devices: (id: string) => ['networks', id, 'devices'] as const,
};

export function useNetworks() {
  return useQuery({
    queryKey: networksKeys.all,
    queryFn: () => api.networks.list(),
  });
}

export function useNetwork(networkId: string | undefined) {
  return useQuery({
    queryKey: networksKeys.detail(networkId ?? ''),
    queryFn: () => api.networks.get(networkId as string),
    enabled: !!networkId,
  });
}

export function useNetworkMembers(networkId: string | undefined) {
  return useQuery({
    queryKey: networksKeys.members(networkId ?? ''),
    queryFn: () => api.networks.members(networkId as string),
    enabled: !!networkId,
  });
}

export function useNetworkDevices(networkId: string | undefined) {
  return useQuery({
    queryKey: networksKeys.devices(networkId ?? ''),
    queryFn: () => api.networks.devices(networkId as string),
    enabled: !!networkId,
    refetchInterval: 15000,
  });
}

export function useCreateNetwork() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: NetworkCreateRequest) => api.networks.create(data),
    onSuccess: () => qc.invalidateQueries({ queryKey: networksKeys.all }),
  });
}

export function useJoinNetwork() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: JoinNetworkRequest) => api.networks.join(data),
    onSuccess: () => qc.invalidateQueries({ queryKey: networksKeys.all }),
  });
}

export function useUpdateNetwork(networkId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: Partial<NetworkCreateRequest>) => api.networks.update(networkId, data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: networksKeys.detail(networkId) });
      qc.invalidateQueries({ queryKey: networksKeys.all });
    },
  });
}

export function useDeleteNetwork() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (networkId: string) => api.networks.remove(networkId),
    onSuccess: () => qc.invalidateQueries({ queryKey: networksKeys.all }),
  });
}

export function useRotateInvite(networkId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.networks.rotateInvite(networkId),
    onSuccess: () => qc.invalidateQueries({ queryKey: networksKeys.detail(networkId) }),
  });
}

export function useUpdateMemberRole(networkId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ userId, role }: { userId: string; role: NetworkRole }) =>
      api.networks.updateMemberRole(networkId, userId, role),
    onSuccess: () => qc.invalidateQueries({ queryKey: networksKeys.members(networkId) }),
  });
}

export function useRemoveMember(networkId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (userId: string) => api.networks.removeMember(networkId, userId),
    onSuccess: () => qc.invalidateQueries({ queryKey: networksKeys.members(networkId) }),
  });
}
