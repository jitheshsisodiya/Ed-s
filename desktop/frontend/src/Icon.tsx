/**
 * A small set of line icons drawn inline.
 *
 * Emoji were the obvious shortcut, but they render as a different picture on
 * every platform — the Windows glyph is a colour monitor on one machine and a
 * grey rectangle on the next — which is exactly the kind of inconsistency
 * that makes an app feel unfinished. These are one stroke weight, one grid,
 * and they inherit their colour from the text around them.
 */

type Name =
  | 'alert'
  | 'copy'
  | 'devices'
  | 'laptop'
  | 'mail'
  | 'monitor'
  | 'moon'
  | 'network'
  | 'phone'
  | 'server'
  | 'settings'
  | 'sun'
  | 'theme-auto'
  | 'user';

const PATHS: Record<Name, string> = {
  alert: 'M12 3.5 21.5 20h-19L12 3.5Z M12 10v4 M12 17.2v.1',
  copy: 'M9 9h9.5v10.5H9V9Z M15 6H5.5v10.5',
  devices: 'M4 5.5h11v8H4v-8Z M7 17h5 M9.5 13.5V17 M18 9.5h2.5v9H18v-9Z',
  laptop: 'M5 6h14v9H5V6Z M3 18h18',
  mail: 'M3.5 6h17v12h-17V6Z M3.5 7l8.5 6 8.5-6',
  monitor: 'M3.5 5h17v11h-17V5Z M9 20h6 M12 16v4',
  moon: 'M20 14.5A8.5 8.5 0 0 1 9.5 4a8.5 8.5 0 1 0 10.5 10.5Z',
  network: 'M12 2.5 20 7v10l-8 4.5L4 17V7l8-4.5Z M12 8.5 15.5 12 12 15.5 8.5 12 12 8.5Z',
  phone: 'M7.5 2.5h9v19h-9v-19Z M10.5 18.5h3',
  server: 'M4 4h16v6H4V4Z M4 14h16v6H4v-6Z M7.5 7h.1 M7.5 17h.1',
  settings:
    'M12 15.2a3.2 3.2 0 1 0 0-6.4 3.2 3.2 0 0 0 0 6.4Z M19.4 14.5a1.7 1.7 0 0 0 .34 1.87l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.7 1.7 0 0 0-2.87 1.2v.17a2 2 0 0 1-4 0v-.09a1.7 1.7 0 0 0-2.93-1.16l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06A1.7 1.7 0 0 0 3.5 13.6h-.17a2 2 0 0 1 0-4h.09A1.7 1.7 0 0 0 4.58 6.6l-.06-.06A2 2 0 1 1 7.35 3.7l.06.06a1.7 1.7 0 0 0 2.87-1.2V2.4a2 2 0 0 1 4 0v.09a1.7 1.7 0 0 0 2.93 1.16l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.7 1.7 0 0 0-.34 1.87v.09a1.7 1.7 0 0 0 1.56 1.03h.17a2 2 0 0 1 0 4h-.09a1.7 1.7 0 0 0-1.56 1.03Z',
  sun: 'M12 16.5a4.5 4.5 0 1 0 0-9 4.5 4.5 0 0 0 0 9Z M12 1.8v2.4 M12 19.8v2.4 M4.8 4.8l1.7 1.7 M17.5 17.5l1.7 1.7 M1.8 12h2.4 M19.8 12h2.4 M4.8 19.2l1.7-1.7 M17.5 6.5l1.7-1.7',
  'theme-auto': 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18Z M12 3v18a9 9 0 0 0 0-18Z',
  user: 'M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8Z M4.5 20.5a7.5 7.5 0 0 1 15 0',
};

/** Maps a device's reported platform onto an icon. */
export function deviceIcon(os: string): Name {
  switch (os) {
    case 'windows':
      return 'monitor';
    case 'windows_server':
      return 'server';
    case 'macos':
    case 'linux':
      return 'laptop';
    case 'android':
    case 'ios':
      return 'phone';
    default:
      return 'devices';
  }
}

export default function Icon({ name, size = 18 }: { name: Name; size?: number }) {
  return (
    <svg
      className="icon"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.6}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
    >
      <path d={PATHS[name]} />
    </svg>
  );
}
