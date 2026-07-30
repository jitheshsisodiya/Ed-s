import type { NetworkRole } from '@/lib/types';

const config: Record<NetworkRole, string> = {
  owner:
    'text-violet-700 bg-violet-50 dark:text-violet-300 dark:bg-violet-950 ring-1 ring-inset ring-violet-200 dark:ring-violet-800',
  admin:
    'text-brand-700 bg-brand-50 dark:text-brand-300 dark:bg-brand-950 ring-1 ring-inset ring-brand-200 dark:ring-brand-800',
  member:
    'text-gray-700 bg-gray-100 dark:text-gray-300 dark:bg-gray-800 ring-1 ring-inset ring-gray-200 dark:ring-gray-700',
};

export function RoleBadge({ role }: { role: NetworkRole }) {
  return (
    <span className={`inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium capitalize ${config[role]}`}>
      {role}
    </span>
  );
}
