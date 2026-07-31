import { useEffect, useState, type FormEvent } from 'react';
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import {
  useDeleteNetwork,
  useNetwork,
  useNetworkDevices,
  useNetworkMembers,
  useRemoveMember,
  useRotateInvite,
  useUpdateMemberRole,
  useUpdateNetwork,
} from '@/hooks/useNetworks';
import { useRemoveDevice } from '@/hooks/useDevices';
import { api, ApiError } from '@/lib/api';
import { useToast } from '@/components/toast-context';
import { Card, ErrorBanner, Spinner } from '@/components/Feedback';
import { Button } from '@/components/Button';
import { Input } from '@/components/Input';
import { Modal } from '@/components/Modal';
import { Tabs } from '@/components/Tabs';
import { RoleBadge } from '@/components/RoleBadge';
import { StatusBadge } from '@/components/StatusBadge';
import { DataTable, type DataTableColumn } from '@/components/DataTable';
import { formatBytes, formatDateTime, formatRelativeTime } from '@/lib/format';
import type { Device, DeviceRegisterRequest, Member, NetworkRole } from '@/lib/types';

const TABS = [
  { key: 'members', label: 'Members' },
  { key: 'devices', label: 'Devices' },
  { key: 'settings', label: 'Settings' },
];

function MembersTab({ networkId, viewerRole }: { networkId: string; viewerRole: NetworkRole }) {
  const { data: members, isLoading, isError, error, refetch } = useNetworkMembers(networkId);
  const { data: network } = useNetwork(networkId);
  const updateRole = useUpdateMemberRole(networkId);
  const removeMember = useRemoveMember(networkId);
  const rotateInvite = useRotateInvite(networkId);
  const toast = useToast();
  const [pendingRemove, setPendingRemove] = useState<Member | null>(null);

  const canManage = viewerRole === 'owner' || viewerRole === 'admin';

  async function handleRoleChange(member: Member, role: NetworkRole) {
    try {
      await updateRole.mutateAsync({ userId: member.userId, role });
      toast.success(`Updated ${member.displayName || member.email}'s role to ${role}.`);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : 'Failed to update role.');
    }
  }

  async function handleRemove() {
    if (!pendingRemove) return;
    try {
      await removeMember.mutateAsync(pendingRemove.userId);
      toast.success(`Removed ${pendingRemove.displayName || pendingRemove.email} from network.`);
      setPendingRemove(null);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : 'Failed to remove member.');
    }
  }

  async function handleRotateInvite() {
    try {
      await rotateInvite.mutateAsync();
      toast.success('Invite code rotated.');
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : 'Failed to rotate invite code.');
    }
  }

  const columns: DataTableColumn<Member>[] = [
    {
      key: 'displayName',
      header: 'Name',
      sortable: true,
      accessor: (m) => m.displayName || m.email,
      render: (m) => (
        <div>
          <p className="font-medium text-gray-900 dark:text-gray-100">{m.displayName || '—'}</p>
          <p className="text-xs text-gray-500 dark:text-gray-400">{m.email}</p>
        </div>
      ),
    },
    {
      key: 'role',
      header: 'Role',
      render: (m) =>
        canManage && m.role !== 'owner' ? (
          <select
            value={m.role}
            onChange={(e) => handleRoleChange(m, e.target.value as NetworkRole)}
            aria-label={`Role for ${m.displayName || m.email}`}
            className="rounded-md border border-gray-300 bg-white px-2 py-1 text-xs dark:border-gray-700 dark:bg-gray-900 dark:text-gray-100"
          >
            <option value="admin">admin</option>
            <option value="member">member</option>
          </select>
        ) : (
          <RoleBadge role={m.role} />
        ),
    },
    {
      key: 'joinedAt',
      header: 'Joined',
      sortable: true,
      accessor: (m) => m.joinedAt,
      render: (m) => formatDateTime(m.joinedAt),
    },
    {
      key: 'actions',
      header: '',
      render: (m) =>
        canManage && m.role !== 'owner' ? (
          <Button variant="ghost" size="sm" onClick={() => setPendingRemove(m)}>
            Remove
          </Button>
        ) : null,
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      {canManage && (
        <Card>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div>
              <p className="text-sm font-medium text-gray-700 dark:text-gray-200">Invite code</p>
              <p className="mt-1 font-mono text-lg text-gray-900 dark:text-gray-100">
                {network?.inviteCode ?? '—'}
              </p>
            </div>
            <Button variant="secondary" onClick={handleRotateInvite} isLoading={rotateInvite.isPending}>
              Rotate invite code
            </Button>
          </div>
        </Card>
      )}

      {isError && (
        <ErrorBanner
          message={error instanceof Error ? error.message : 'Failed to load members.'}
          onRetry={() => refetch()}
        />
      )}

      {!isError && (
        <DataTable
          columns={columns}
          data={members ?? []}
          keyField={(m) => m.userId}
          isLoading={isLoading}
          emptyMessage="No members found."
        />
      )}

      <Modal
        isOpen={pendingRemove !== null}
        onClose={() => setPendingRemove(null)}
        title="Remove member"
        footer={
          <>
            <Button variant="secondary" onClick={() => setPendingRemove(null)}>
              Cancel
            </Button>
            <Button variant="danger" onClick={handleRemove} isLoading={removeMember.isPending}>
              Remove
            </Button>
          </>
        }
      >
        <p className="text-sm text-gray-600 dark:text-gray-300">
          Remove <strong>{pendingRemove?.displayName || pendingRemove?.email}</strong> from this
          network? They will lose access to all devices on this network.
        </p>
      </Modal>
    </div>
  );
}

function RegisterDeviceModal({
  networkId,
  isOpen,
  onClose,
}: {
  networkId: string;
  isOpen: boolean;
  onClose: () => void;
}) {
  const toast = useToast();
  const [form, setForm] = useState<DeviceRegisterRequest>({ name: '', os: '', osVersion: '', publicKey: '' });
  const [errors, setErrors] = useState<Partial<Record<keyof DeviceRegisterRequest, string>>>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);

  function reset() {
    setForm({ name: '', os: '', osVersion: '', publicKey: '' });
    setErrors({});
    setFormError(null);
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setFormError(null);
    const fieldErrors: typeof errors = {};
    if (!form.name.trim()) fieldErrors.name = 'Device name is required.';
    if (!form.os.trim()) fieldErrors.os = 'Operating system is required.';
    if (!form.publicKey.trim()) fieldErrors.publicKey = 'WireGuard public key is required.';
    setErrors(fieldErrors);
    if (Object.keys(fieldErrors).length > 0) return;

    setIsSubmitting(true);
    try {
      await api.networks.registerDevice(networkId, {
        name: form.name.trim(),
        os: form.os.trim(),
        osVersion: form.osVersion?.trim() || undefined,
        publicKey: form.publicKey.trim(),
      });
      toast.success(`Device "${form.name.trim()}" registered.`);
      reset();
      onClose();
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : 'Failed to register device.');
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <Modal
      isOpen={isOpen}
      onClose={() => {
        reset();
        onClose();
      }}
      title="Register device"
    >
      <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-4">
        {formError && <ErrorBanner message={formError} />}
        <Input
          label="Device name"
          value={form.name}
          onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
          error={errors.name}
          required
        />
        <Input
          label="Operating system"
          placeholder="linux, windows, macos…"
          value={form.os}
          onChange={(e) => setForm((f) => ({ ...f, os: e.target.value }))}
          error={errors.os}
          required
        />
        <Input
          label="OS version"
          value={form.osVersion}
          onChange={(e) => setForm((f) => ({ ...f, osVersion: e.target.value }))}
        />
        <Input
          label="WireGuard public key"
          value={form.publicKey}
          onChange={(e) => setForm((f) => ({ ...f, publicKey: e.target.value }))}
          error={errors.publicKey}
          required
        />
        <div className="mt-2 flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" isLoading={isSubmitting}>
            Register
          </Button>
        </div>
      </form>
    </Modal>
  );
}

function DevicesTab({ networkId }: { networkId: string }) {
  const { data: devices, isLoading, isError, error, refetch } = useNetworkDevices(networkId);
  const removeDevice = useRemoveDevice();
  const toast = useToast();
  const [pendingRemove, setPendingRemove] = useState<Device | null>(null);
  const [registerOpen, setRegisterOpen] = useState(false);

  async function handleRemove() {
    if (!pendingRemove) return;
    try {
      await removeDevice.mutateAsync(pendingRemove.id);
      toast.success(`Removed device "${pendingRemove.name}".`);
      setPendingRemove(null);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : 'Failed to remove device.');
    }
  }

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
    {
      key: 'actions',
      header: '',
      render: (d) => (
        <Button variant="ghost" size="sm" onClick={() => setPendingRemove(d)}>
          Remove
        </Button>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <div className="flex justify-end">
        <Button onClick={() => setRegisterOpen(true)}>Register device</Button>
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
          data={devices ?? []}
          keyField={(d) => d.id}
          isLoading={isLoading}
          emptyMessage="No devices registered on this network yet."
        />
      )}

      <RegisterDeviceModal networkId={networkId} isOpen={registerOpen} onClose={() => setRegisterOpen(false)} />

      <Modal
        isOpen={pendingRemove !== null}
        onClose={() => setPendingRemove(null)}
        title="Remove device"
        footer={
          <>
            <Button variant="secondary" onClick={() => setPendingRemove(null)}>
              Cancel
            </Button>
            <Button variant="danger" onClick={handleRemove} isLoading={removeDevice.isPending}>
              Remove
            </Button>
          </>
        }
      >
        <p className="text-sm text-gray-600 dark:text-gray-300">
          Remove device <strong>{pendingRemove?.name}</strong> from this network? It will need to
          re-register to reconnect.
        </p>
      </Modal>
    </div>
  );
}

function SettingsTab({ networkId, canEdit, isOwner }: { networkId: string; canEdit: boolean; isOwner: boolean }) {
  const { data: network, isLoading } = useNetwork(networkId);
  const updateNetwork = useUpdateNetwork(networkId);
  const deleteNetwork = useDeleteNetwork();
  const toast = useToast();
  const navigate = useNavigate();

  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [cidr, setCidr] = useState('');
  const [dnsServers, setDnsServers] = useState('');
  const [initialized, setInitialized] = useState(false);
  const [errors, setErrors] = useState<{ name?: string; cidr?: string; dnsServers?: string }>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleteConfirmText, setDeleteConfirmText] = useState('');

  useEffect(() => {
    if (network && !initialized) {
      setName(network.name);
      setDescription(network.description ?? '');
      setCidr(network.cidr);
      setDnsServers((network.dnsServers ?? []).join(', '));
      setInitialized(true);
    }
  }, [network, initialized]);

  const CIDR_RE = /^(\d{1,3}\.){3}\d{1,3}\/\d{1,2}$/;
  const IP_RE = /^(\d{1,3}\.){3}\d{1,3}$/;

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setFormError(null);
    const fieldErrors: typeof errors = {};
    if (!name.trim()) fieldErrors.name = 'Network name is required.';
    if (!cidr.trim()) fieldErrors.cidr = 'CIDR block is required.';
    else if (!CIDR_RE.test(cidr.trim())) fieldErrors.cidr = 'Enter a valid CIDR, e.g. 10.77.0.0/24.';
    const dnsList = dnsServers.split(',').map((s) => s.trim()).filter(Boolean);
    if (dnsList.some((ip) => !IP_RE.test(ip))) {
      fieldErrors.dnsServers = 'Enter a comma-separated list of valid IPv4 addresses.';
    }
    setErrors(fieldErrors);
    if (Object.keys(fieldErrors).length > 0) return;

    try {
      await updateNetwork.mutateAsync({
        name: name.trim(),
        description: description.trim() || undefined,
        cidr: cidr.trim(),
        dnsServers: dnsList,
      });
      toast.success('Network settings saved.');
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : 'Failed to save network settings.');
    }
  }

  async function handleDelete() {
    try {
      await deleteNetwork.mutateAsync(networkId);
      toast.success('Network deleted.');
      navigate('/networks', { replace: true });
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : 'Failed to delete network.');
    }
  }

  if (isLoading || !network) return <Spinner label="Loading settings…" />;

  return (
    <div className="flex flex-col gap-6">
      <Card>
        <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-4">
          {formError && <ErrorBanner message={formError} />}
          <Input
            label="Network name"
            value={name}
            onChange={(e) => setName(e.target.value)}
            error={errors.name}
            disabled={!canEdit}
            required
          />
          <Input
            label="Description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            disabled={!canEdit}
          />
          <Input
            label="CIDR block"
            value={cidr}
            onChange={(e) => setCidr(e.target.value)}
            error={errors.cidr}
            disabled={!canEdit}
            required
          />
          <Input
            label="DNS servers"
            placeholder="1.1.1.1, 8.8.8.8"
            value={dnsServers}
            onChange={(e) => setDnsServers(e.target.value)}
            error={errors.dnsServers}
            hint="Comma-separated IPv4 addresses pushed to devices on this network."
            disabled={!canEdit}
          />
          {canEdit && (
            <div className="flex justify-end">
              <Button type="submit" isLoading={updateNetwork.isPending}>
                Save changes
              </Button>
            </div>
          )}
        </form>
      </Card>

      {isOwner && (
        <Card className="border-red-200 dark:border-red-900">
          <h3 className="text-sm font-semibold text-red-700 dark:text-red-400">Danger zone</h3>
          <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
            Deleting this network removes all its devices, members and logs. This cannot be
            undone.
          </p>
          <div className="mt-4">
            <Button variant="danger" onClick={() => setDeleteOpen(true)}>
              Delete network
            </Button>
          </div>
        </Card>
      )}

      <Modal
        isOpen={deleteOpen}
        onClose={() => {
          setDeleteOpen(false);
          setDeleteConfirmText('');
        }}
        title="Delete network"
        footer={
          <>
            <Button
              variant="secondary"
              onClick={() => {
                setDeleteOpen(false);
                setDeleteConfirmText('');
              }}
            >
              Cancel
            </Button>
            <Button
              variant="danger"
              onClick={handleDelete}
              isLoading={deleteNetwork.isPending}
              disabled={deleteConfirmText !== network.name}
            >
              Delete permanently
            </Button>
          </>
        }
      >
        <p className="text-sm text-gray-600 dark:text-gray-300">
          Type <strong>{network.name}</strong> to confirm deletion.
        </p>
        <input
          value={deleteConfirmText}
          onChange={(e) => setDeleteConfirmText(e.target.value)}
          aria-label="Confirm network name"
          className="mt-3 w-full rounded-md border border-gray-300 px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-900 dark:text-gray-100"
        />
      </Modal>
    </div>
  );
}

export default function NetworkDetailPage() {
  const { id } = useParams<{ id: string }>();
  const [searchParams, setSearchParams] = useSearchParams();
  const activeTab = searchParams.get('tab') ?? 'members';

  const { data: network, isLoading, isError, error, refetch } = useNetwork(id);

  if (!id) return <ErrorBanner message="Missing network id." />;
  if (isLoading) return <Spinner label="Loading network…" />;
  if (isError || !network) {
    return (
      <ErrorBanner
        message={error instanceof Error ? error.message : 'Failed to load network.'}
        onRetry={() => refetch()}
      />
    );
  }

  const canEdit = network.role === 'owner' || network.role === 'admin';

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <Link to="/networks" className="text-sm text-brand-600 hover:text-brand-700 dark:text-brand-400">
            &larr; All networks
          </Link>
          <div className="mt-1 flex items-center gap-2">
            <h2 className="text-2xl font-semibold text-gray-900 dark:text-gray-100">{network.name}</h2>
            <RoleBadge role={network.role} />
          </div>
          <p className="mt-1 text-sm text-gray-500 dark:text-gray-400">
            {network.cidr} &middot; {network.memberCount} members &middot; {network.deviceCount} devices
          </p>
        </div>
      </div>

      <Tabs tabs={TABS} active={activeTab} onChange={(key) => setSearchParams({ tab: key })} />

      {activeTab === 'members' && <MembersTab networkId={id} viewerRole={network.role} />}
      {activeTab === 'devices' && <DevicesTab networkId={id} />}
      {activeTab === 'settings' && (
        <SettingsTab networkId={id} canEdit={canEdit} isOwner={network.role === 'owner'} />
      )}
    </div>
  );
}
