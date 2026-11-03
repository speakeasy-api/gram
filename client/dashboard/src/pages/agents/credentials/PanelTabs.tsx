import {
  PageTabsList,
  PageTabsTrigger,
  Tabs,
  TabsContent,
} from "@/components/ui/Tabs";
import { cn } from "@/lib/utils";
import type { ComponentProps, JSX, ReactNode } from "react";

/**
 * A step's alternatives as tabs in one bordered panel: the tab bar in a muted
 * strip across the top, the chosen tab's content below it, inside the same
 * border. aria-label names the tab list; other props go to the panel itself.
 */
export function PanelTabs<T extends string>({
  value,
  onValueChange,
  tabs,
  children,
  className,
  "aria-label": tabsLabel,
  ...panel
}: {
  value: T;
  onValueChange: (value: T) => void;
  tabs: { value: T; label: ReactNode }[];
  /** The PanelTab for each tab. */
  children: ReactNode;
} & Omit<ComponentProps<"div">, "children">): JSX.Element {
  return (
    <div className={cn("border-border border", className)} {...panel}>
      <Tabs
        value={value}
        onValueChange={(next) => {
          const tab = tabs.find((candidate) => candidate.value === next);
          if (tab !== undefined) onValueChange(tab.value);
        }}
        className="gap-0"
      >
        <div className="border-border bg-muted/30 border-b px-4">
          <PageTabsList aria-label={tabsLabel}>
            {tabs.map((tab) => (
              <PageTabsTrigger key={tab.value} value={tab.value}>
                {tab.label}
              </PageTabsTrigger>
            ))}
          </PageTabsList>
        </div>
        {children}
      </Tabs>
    </div>
  );
}

/**
 * One tab's content in a PanelTabs panel. It stays mounted while another tab
 * is shown, so what it holds survives switching away and back.
 */
export function PanelTab({
  value,
  children,
}: {
  value: string;
  children: ReactNode;
}): JSX.Element {
  return (
    <TabsContent
      value={value}
      forceMount
      className="data-[state=inactive]:hidden"
    >
      <div className="space-y-4 p-4">{children}</div>
    </TabsContent>
  );
}
