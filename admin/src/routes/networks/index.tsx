import { useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { useNetworks, useCreateNetwork, useJoinNetwork } from '@/hooks/useNetworks';
import { Card, EmptyState, ErrorBanner, Spinner } from '@/components/Feedback';
import { Button } from '@/components/Button';
import { Input } from '@/components/Input';
import { Modal } from '@/components/Modal';
import { RoleBadge } from '@/components/RoleBadge';
import { useToast } from '@/components/Toast';
import { ApiError } from '@/lib/api';

const CIDR_RE = /^(\d{1,3}\.){3}\d{1,3}\/\d{1,2}$/;

function CreateNetworkModal({ isOpen, onClose }: { isOpen: boolean; onClose: () => void }) {
  const createNetwork = useCreateNetwork();
  const toast = useToast();

  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [cidr, setCidr] = useState('');
  const [dnsServers, setDnsServers] = useState('');
  const [errors, setErrors] = useState<{ name?: string; cidr?: string; dnsServers?: string }>({});
  const [formError, setFormError] = useState<string | null>(null);

  function reset() {
    setName('');
    setDescription('');
    setCidr('');
    setDnsServers('');
    setErrors({});
    setFormError(null);
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setFormError(null);
    const fieldErrors: typeof errors = {};
    if (!name.trim()) fieldErrors.name = 'Network name is required.';
    if (!cidr.trim()) fieldErrors.cidr = 'CIDR block is required.';
    else if (!CIDR_RE.test(cidr.trim())) fieldErrors.cidr = 'Enter a valid CIDR, e.g. 10.77.0.0/24.';

    const dnsList = dnsServers
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean);
    const ipRe = /^(\d{1,3}\.){3}\d{1,3}$/;
    if (dnsList.some((ip) => !ipRe.test(ip))) {
      fieldErrors.dnsServers = 'Enter a comma-separated list of valid IPv4 addresses.';
    }

    setErrors(fieldErrors);
    if (Object.keys(fieldErrors).length > 0) return;

    try {
      await createNetwork.mutateAsync({
        name: name.trim(),
        description: description.trim() || undefined,
        cidr: cidr.trim(),
        dnsServers: dnsList.length > 0 ? dnsList : undefined,
      });
      toast.success(`Network "${name.trim()}" created.`);
      reset();
      onClose();
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : 'Failed to create network.');
    }
  }

  return (
    <Modal
      isOpen={isOpen}
      onClose={() => {
        reset();
        onClose();
      }}
      title="Create network"
    >
      <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-4">
        {formError && <ErrorBanner message={formError} />}
        <Input label="Name" value={name} onChange={(e) => setName(e.target.value)} error={errors.name} required />
        <Input
          label="Description"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
        />
        <Input
          label="CIDR block"
          placeholder="10.77.0.0/24"
          value={cidr}
          onChange={(e) => setCidr(e.target.value)}
          error={errors.cidr}
          required
        />
        <Input
          label="DNS servers"
          placeholder="1.1.1.1, 8.8.8.8"
          value={dnsServers}
          onChange={(e) => setDnsServers(e.target.value)}
          error={errors.dnsServers}
          hint="Optional, comma-separated."
        />
        <div className="mt-2 flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" isLoading={createNetwork.isPending}>
            Create network
          </Button>
        </div>
      </form>
    </Modal>
  );
}

function JoinNetworkModal({ isOpen, onClose }: { isOpen: boolean; onClose: () => void }) {
  const joinNetwork = useJoinNetwork();
  const toast = useToast();

  const [inviteCode, setInviteCode] = useState('');
  const [error, setError] = useState<string | undefined>();
  const [formError, setFormError] = useState<string | null>(null);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setFormError(null);
    if (!inviteCode.trim()) {
      setError('Invite code is required.');
      return;
    }
    setError(undefined);
    try {
      const network = await joinNetwork.mutateAsync({ inviteCode: inviteCode.trim() });
      toast.success(`Joined "${network.name}".`);
      setInviteCode('');
      onClose();
    } catch (err) {
      setFormError(err instanceof ApiError ? err.message : 'Failed to join network. Check the invite code.');
    }
  }

  return (
    <Modal
      isOpen={isOpen}
      onClose={() => {
        setInviteCode('');
        setFormError(null);
        onClose();
      }}
      title="Join a network"
    >
      <form onSubmit={handleSubmit} noValidate className="flex flex-col gap-4">
        {formError && <ErrorBanner message={formError} />}
        <Input
          label="Invite code"
          value={inviteCode}
          onChange={(e) => setInviteCode(e.target.value)}
          error={error}
          required
        />
        <div className="mt-2 flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" isLoading={joinNetwork.isPending}>
            Join network
          </Button>
        </div>
      </form>
    </Modal>
  );
}

export default function NetworksIndexPage() {
  const { data, isLoading, isError, error, refetch } = useNetworks();
  const [createOpen, setCreateOpen] = useState(false);
  const [joinOpen, setJoinOpen] = useState(false);

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-sm text-gray-500 dark:text-gray-400">
          Networks you own, administer, or belong to.
        </p>
        <div className="flex gap-2">
          <Button variant="secondary" onClick={() => setJoinOpen(true)}>
            Join by invite code
          </Button>
          <Button onClick={() => setCreateOpen(true)}>New network</Button>
        </div>
      </div>

      {isLoading && <Spinner label="Loading networks…" />}

      {isError && (
        <ErrorBanner
          message={error instanceof Error ? error.message : 'Failed to load networks.'}
          onRetry={() => refetch()}
        />
      )}

      {!isLoading && !isError && data && data.length === 0 && (
        <EmptyState
          title="No networks yet"
          description="Create a new mesh network or join one with an invite code."
          action={
            <div className="flex gap-2">
              <Button variant="secondary" onClick={() => setJoinOpen(true)}>
                Join by invite code
              </Button>
              <Button onClick={() => setCreateOpen(true)}>New network</Button>
            </div>
          }
        />
      )}

      {!isLoading && !isError && data && data.length > 0 && (
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {data.map((network) => (
            <Link key={network.id} to={`/networks/${network.id}`} className="block">
              <Card className="h-full transition-shadow hover:shadow-md">
                <div className="flex items-start justify-between gap-2">
                  <h3 className="font-semibold text-gray-900 dark:text-gray-100">{network.name}</h3>
                  <RoleBadge role={network.role} />
                </div>
                {network.description && (
                  <p className="mt-1 line-clamp-2 text-sm text-gray-500 dark:text-gray-400">
                    {network.description}
                  </p>
                )}
                <dl className="mt-4 grid grid-cols-3 gap-2 text-center text-xs">
                  <div>
                    <dt className="text-gray-400 dark:text-gray-500">CIDR</dt>
                    <dd className="mt-0.5 font-medium text-gray-700 dark:text-gray-200">{network.cidr}</dd>
                  </div>
                  <div>
                    <dt className="text-gray-400 dark:text-gray-500">Members</dt>
                    <dd className="mt-0.5 font-medium text-gray-700 dark:text-gray-200">{network.memberCount}</dd>
                  </div>
                  <div>
                    <dt className="text-gray-400 dark:text-gray-500">Devices</dt>
                    <dd className="mt-0.5 font-medium text-gray-700 dark:text-gray-200">{network.deviceCount}</dd>
                  </div>
                </dl>
              </Card>
            </Link>
          ))}
        </div>
      )}

      <CreateNetworkModal isOpen={createOpen} onClose={() => setCreateOpen(false)} />
      <JoinNetworkModal isOpen={joinOpen} onClose={() => setJoinOpen(false)} />
    </div>
  );
}
