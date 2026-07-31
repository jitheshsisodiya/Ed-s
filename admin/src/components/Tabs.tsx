interface TabsProps {
  tabs: { key: string; label: string }[];
  active: string;
  onChange: (key: string) => void;
}

export function Tabs({ tabs, active, onChange }: TabsProps) {
  return (
    <div className="border-b border-gray-200 dark:border-gray-800" role="tablist">
      <nav className="-mb-px flex gap-6">
        {tabs.map((tab) => (
          <button
            key={tab.key}
            type="button"
            role="tab"
            aria-selected={active === tab.key}
            onClick={() => onChange(tab.key)}
            className={`whitespace-nowrap border-b-2 px-1 py-3 text-sm font-medium transition-colors ${
              active === tab.key
                ? 'border-brand-600 text-brand-600 dark:text-brand-400'
                : 'border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200'
            }`}
          >
            {tab.label}
          </button>
        ))}
      </nav>
    </div>
  );
}
