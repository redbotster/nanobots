import * as RadixTabs from "@radix-ui/react-tabs";
import type { ReactNode } from "react";

export function Tabs({
  tabs,
  defaultValue,
  value,
  onValueChange,
  right,
}: {
  tabs: { value: string; label: string; content: ReactNode }[];
  defaultValue: string;
  /** Pass both value and onValueChange to drive the active tab externally
   * (e.g. auto-switching to "results" when a run finishes) while still
   * letting the person click other tabs freely afterward — it's a normal
   * controlled component, not a one-time jump. */
  value?: string;
  onValueChange?: (value: string) => void;
  right?: ReactNode;
}) {
  return (
    <RadixTabs.Root
      defaultValue={defaultValue}
      value={value}
      onValueChange={onValueChange}
      className="flex h-full flex-col"
    >
      <div className="flex items-center border-b border-edge">
        <RadixTabs.List className="flex">
          {tabs.map((t) => (
            <RadixTabs.Trigger
              key={t.value}
              value={t.value}
              className="border-b-2 border-transparent px-4 py-2.5 font-display text-sm text-muted transition-colors data-[state=active]:border-tron data-[state=active]:text-ink hover:text-ink"
            >
              {t.label}
            </RadixTabs.Trigger>
          ))}
        </RadixTabs.List>
        {right && <div className="ml-auto pr-3 text-xs text-muted">{right}</div>}
      </div>
      {tabs.map((t) => (
        <RadixTabs.Content
          key={t.value}
          value={t.value}
          className="min-h-0 flex-1 overflow-auto"
        >
          {t.content}
        </RadixTabs.Content>
      ))}
    </RadixTabs.Root>
  );
}
