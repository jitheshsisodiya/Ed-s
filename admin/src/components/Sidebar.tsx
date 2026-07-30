import type { SVGProps } from 'react';
import { NavLink } from 'react-router-dom';

const navItems = [
  { to: '/dashboard', label: 'Dashboard', icon: DashboardIcon },
  { to: '/networks', label: 'Networks', icon: NetworkIcon },
  { to: '/devices', label: 'Devices', icon: DeviceIcon },
  { to: '/logs/audit', label: 'Audit Logs', icon: LogIcon },
  { to: '/logs/connections', label: 'Connection Logs', icon: PlugIcon },
  { to: '/settings/account', label: 'Account', icon: UserIcon },
];

export function Sidebar({ onNavigate }: { onNavigate?: () => void }) {
  return (
    <nav className="flex h-full w-64 flex-col border-r border-gray-200 bg-white dark:border-gray-800 dark:bg-gray-900" aria-label="Primary">
      <div className="flex h-16 items-center gap-2 border-b border-gray-200 px-5 dark:border-gray-800">
        <div className="flex h-8 w-8 items-center justify-center rounded-md bg-brand-600 text-sm font-bold text-white">
          N
        </div>
        <span className="text-base font-semibold text-gray-900 dark:text-gray-100">NexusVPN</span>
      </div>
      <ul className="flex-1 space-y-1 overflow-y-auto p-3">
        {navItems.map((item) => (
          <li key={item.to}>
            <NavLink
              to={item.to}
              onClick={onNavigate}
              className={({ isActive }) =>
                `flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors ${
                  isActive
                    ? 'bg-brand-50 text-brand-700 dark:bg-brand-950 dark:text-brand-300'
                    : 'text-gray-600 hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-800 dark:hover:text-gray-100'
                }`
              }
            >
              <item.icon className="h-4.5 w-4.5 shrink-0" />
              {item.label}
            </NavLink>
          </li>
        ))}
      </ul>
      <div className="border-t border-gray-200 p-3 text-xs text-gray-400 dark:border-gray-800 dark:text-gray-500">
        NexusVPN Admin
      </div>
    </nav>
  );
}

function DashboardIcon(props: SVGProps<SVGSVGElement>) {
  return (
    <svg viewBox="0 0 20 20" fill="currentColor" {...props}>
      <path d="M3 3h6v6H3V3zm8 0h6v10h-6V3zM3 11h6v6H3v-6z" />
    </svg>
  );
}
function NetworkIcon(props: SVGProps<SVGSVGElement>) {
  return (
    <svg viewBox="0 0 20 20" fill="currentColor" {...props}>
      <path d="M10 2a2 2 0 100 4 2 2 0 000-4zM4 14a2 2 0 100 4 2 2 0 000-4zm12 0a2 2 0 100 4 2 2 0 000-4zM10 7v3m0 0l-4 3m4-3l4 3" stroke="currentColor" strokeWidth="1" fill="none" />
    </svg>
  );
}
function DeviceIcon(props: SVGProps<SVGSVGElement>) {
  return (
    <svg viewBox="0 0 20 20" fill="currentColor" {...props}>
      <path fillRule="evenodd" d="M4 4a2 2 0 00-2 2v6a2 2 0 002 2h3l-1 2h8l-1-2h3a2 2 0 002-2V6a2 2 0 00-2-2H4zm0 2h12v6H4V6z" clipRule="evenodd" />
    </svg>
  );
}
function LogIcon(props: SVGProps<SVGSVGElement>) {
  return (
    <svg viewBox="0 0 20 20" fill="currentColor" {...props}>
      <path fillRule="evenodd" d="M4 3a1 1 0 00-1 1v12a1 1 0 001 1h12a1 1 0 001-1V4a1 1 0 00-1-1H4zm2 4a1 1 0 000 2h8a1 1 0 100-2H6zm0 4a1 1 0 100 2h5a1 1 0 100-2H6z" clipRule="evenodd" />
    </svg>
  );
}
function PlugIcon(props: SVGProps<SVGSVGElement>) {
  return (
    <svg viewBox="0 0 20 20" fill="currentColor" {...props}>
      <path d="M6 2a1 1 0 011 1v3h2V3a1 1 0 112 0v3a3 3 0 013 3v1a5 5 0 01-4 4.9V17a1 1 0 11-2 0v-2.1A5 5 0 014 9V8a3 3 0 013-3V3a1 1 0 011-1z" />
    </svg>
  );
}
function UserIcon(props: SVGProps<SVGSVGElement>) {
  return (
    <svg viewBox="0 0 20 20" fill="currentColor" {...props}>
      <path fillRule="evenodd" d="M10 9a3 3 0 100-6 3 3 0 000 6zm-7 9a7 7 0 1114 0H3z" clipRule="evenodd" />
    </svg>
  );
}
