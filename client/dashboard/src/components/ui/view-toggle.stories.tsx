import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";
import { ViewToggle } from "@/components/ui/view-toggle";
import { useViewMode, type ViewMode } from "@/components/ui/use-view-mode";

/**
 * `ViewToggle` itself is a controlled component (`value` / `onChange`) — the
 * persistence lives in the paired `useViewMode` hook, which stores the mode in
 * `localStorage` (not the URL, despite the name suggesting route state), so no
 * router/nuqs adapter is needed here. `Persisted` below uses the real hook.
 */
const meta: Meta<typeof ViewToggle> = {
  title: "UI/ViewToggle",
  component: ViewToggle,
  parameters: {
    layout: "padded",
  },
};

export default meta;

type Story = StoryObj<typeof ViewToggle>;

function ControlledToggle({ initial }: { initial: ViewMode }) {
  const [value, setValue] = useState<ViewMode>(initial);
  return <ViewToggle value={value} onChange={setValue} />;
}

export const GridSelected: Story = {
  render: () => <ControlledToggle initial="grid" />,
};

export const TableSelected: Story = {
  render: () => <ControlledToggle initial="table" />,
};

function PersistedToggle() {
  const [mode, setMode] = useViewMode();
  return (
    <div className="flex flex-col items-start gap-2">
      <ViewToggle value={mode} onChange={setMode} />
      <p className="text-muted-foreground text-xs">
        Persisted to localStorage under "gram-view-mode" — reload the story to
        see it stick.
      </p>
    </div>
  );
}

export const Persisted: Story = {
  render: () => <PersistedToggle />,
};

function ToolbarHeightToggle() {
  const [value, setValue] = useState<ViewMode>("grid");
  return (
    <div className="border-border bg-muted/40 flex items-center gap-3 rounded-lg border p-2">
      <span className="text-muted-foreground text-sm">24 servers</span>
      <ViewToggle value={value} onChange={setValue} itemClassName="h-10" />
    </div>
  );
}

export const InToolbarHeight: Story = {
  render: () => <ToolbarHeightToggle />,
};
