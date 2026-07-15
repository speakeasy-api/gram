import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";
import { ChevronsUpDown } from "lucide-react";

import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";

const meta: Meta<typeof Collapsible> = {
  title: "UI/Collapsible",
  component: Collapsible,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof Collapsible>;

export const Default: Story = {
  render: () => (
    <Collapsible defaultOpen className="w-72">
      <CollapsibleTrigger className="flex w-full items-center justify-between rounded-md border px-3 py-2 text-sm font-medium">
        Request headers
        <ChevronsUpDown className="size-4" />
      </CollapsibleTrigger>
      <CollapsibleContent className="mt-2 rounded-md border px-3 py-2 text-sm">
        Authorization: Bearer •••••••
      </CollapsibleContent>
    </Collapsible>
  ),
};

function ControlledCollapsible() {
  const [open, setOpen] = useState(false);

  return (
    <Collapsible open={open} onOpenChange={setOpen} className="w-72">
      <CollapsibleTrigger className="flex w-full items-center justify-between rounded-md border px-3 py-2 text-sm font-medium">
        {open ? "Hide" : "Show"} 3 more tools
        <ChevronsUpDown className="size-4" />
      </CollapsibleTrigger>
      <CollapsibleContent className="mt-2 flex flex-col gap-1 rounded-md border px-3 py-2 text-sm">
        <span>get_weather</span>
        <span>list_forecasts</span>
        <span>get_alerts</span>
      </CollapsibleContent>
    </Collapsible>
  );
}

export const Controlled: Story = {
  render: () => <ControlledCollapsible />,
};

export const Disabled: Story = {
  render: () => (
    <Collapsible disabled className="w-72">
      <CollapsibleTrigger className="flex w-full items-center justify-between rounded-md border px-3 py-2 text-sm font-medium opacity-50">
        Unavailable section
        <ChevronsUpDown className="size-4" />
      </CollapsibleTrigger>
      <CollapsibleContent className="mt-2 rounded-md border px-3 py-2 text-sm">
        This content cannot be toggled.
      </CollapsibleContent>
    </Collapsible>
  ),
};

export const NestedInList: Story = {
  render: () => (
    <div className="flex w-80 flex-col gap-2">
      <div className="rounded-md border px-3 py-2 text-sm">tool_a</div>
      <div className="rounded-md border px-3 py-2 text-sm">tool_b</div>
      <Collapsible className="rounded-md border px-3 py-2">
        <CollapsibleTrigger className="flex w-full items-center justify-between text-sm font-medium">
          View error details
          <ChevronsUpDown className="size-4" />
        </CollapsibleTrigger>
        <CollapsibleContent className="text-muted-foreground mt-2 text-xs">
          Request timed out after 30s while calling the upstream API.
        </CollapsibleContent>
      </Collapsible>
    </div>
  ),
};
