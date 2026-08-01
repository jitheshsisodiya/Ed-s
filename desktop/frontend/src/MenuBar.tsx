import { useEffect, useRef, useState, type ReactNode } from 'react';
import { motion } from 'framer-motion';

/**
 * A classic application menu bar.
 *
 * Radmin has one and it is the right call for this kind of app: the actions
 * behind it — create a network, change your name, quit — are things you do
 * once and then never look for again, and a menu bar is where a desktop user
 * already knows to look for exactly that class of thing. Putting them on the
 * main surface as buttons would compete with the one control that matters.
 *
 * It behaves the way menu bars behave: click to open, then hovering a
 * sibling switches to it without another click.
 */
export default function MenuBar({ menus }: { menus: Menu[] }) {
  const [open, setOpen] = useState<string | null>(null);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(null);
    };
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(null);
    window.addEventListener('mousedown', close);
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('mousedown', close);
      window.removeEventListener('keydown', onKey);
    };
  }, [open]);

  return (
    <div
      ref={ref}
      className="flex items-center"
      style={{ ['--wails-draggable' as string]: 'no-drag' }}
    >
      {menus.map((menu) => (
        <div key={menu.label} className="relative">
          <button
            onClick={() => setOpen((o) => (o === menu.label ? null : menu.label))}
            onMouseEnter={() => open && setOpen(menu.label)}
            className={`px-2.5 py-1 text-[11.5px] transition-colors ${
              open === menu.label ? 'bg-live/12 text-live' : 'text-ink-dim hover:text-ink'
            }`}
          >
            {menu.label}
          </button>

          {open === menu.label && (
            <motion.div
              role="menu"
              initial={{ opacity: 0, y: -3 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: 0.09 }}
              className="panel absolute left-0 top-full z-50 min-w-[210px] py-1"
            >
              {menu.items.map((item, i) =>
                item.separator ? (
                  <div key={i} className="my-1 border-t border-deck-line" />
                ) : (
                  <button
                    key={i}
                    role="menuitem"
                    disabled={item.disabled}
                    onClick={() => {
                      setOpen(null);
                      item.onSelect();
                    }}
                    className="flex w-full items-center gap-3 px-3 py-1.5 text-left text-[12px] text-ink-dim transition-colors hover:bg-live/12 hover:text-ink disabled:opacity-35 disabled:hover:bg-transparent"
                  >
                    <span className="flex-1">{item.label}</span>
                    {item.hint && (
                      <span className="font-mono text-[10px] text-ink-faint">{item.hint}</span>
                    )}
                    {item.checked && <span className="text-live">✓</span>}
                  </button>
                ),
              )}
            </motion.div>
          )}
        </div>
      ))}
    </div>
  );
}

export interface Menu {
  label: string;
  items: MenuEntry[];
}

export type MenuEntry =
  | { separator: true }
  | {
      label: string;
      /** Keyboard shortcut shown right-aligned, as a menu bar does. */
      hint?: string;
      checked?: boolean;
      disabled?: boolean;
      onSelect: () => void;
      separator?: false;
    };

/** Renders nothing; exists so callers can build entries inline readably. */
export function item(
  label: string,
  onSelect: () => void,
  extra?: { hint?: string; disabled?: boolean; checked?: boolean },
): MenuEntry {
  return { label, onSelect, ...extra };
}

export const separator: MenuEntry = { separator: true };

export type { ReactNode };
