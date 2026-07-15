import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import {
  Popover,
  PopoverAnchor,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";

const meta: Meta<typeof Popover> = {
  title: "UI/Popover",
  component: Popover,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof Popover>;

export const Default: Story = {
  render: () => (
    <Popover>
      <PopoverTrigger asChild>
        <button className="rounded-md border px-3 py-1.5 text-sm">
          Open popover
        </button>
      </PopoverTrigger>
      <PopoverContent>
        <p className="text-sm font-medium">Popover title</p>
        <p className="text-muted-foreground mt-1 text-sm">
          Additional detail rendered inside the popover content.
        </p>
      </PopoverContent>
    </Popover>
  ),
};

export const AlignStart: Story = {
  render: () => (
    <Popover>
      <PopoverTrigger asChild>
        <button className="rounded-md border px-3 py-1.5 text-sm">
          Align start
        </button>
      </PopoverTrigger>
      <PopoverContent align="start">
        <p className="text-sm">Aligned to the start of the trigger.</p>
      </PopoverContent>
    </Popover>
  ),
};

export const AlignEnd: Story = {
  render: () => (
    <Popover>
      <PopoverTrigger asChild>
        <button className="rounded-md border px-3 py-1.5 text-sm">
          Align end
        </button>
      </PopoverTrigger>
      <PopoverContent align="end">
        <p className="text-sm">Aligned to the end of the trigger.</p>
      </PopoverContent>
    </Popover>
  ),
};

export const WithAnchor: Story = {
  render: () => (
    <Popover>
      <PopoverAnchor asChild>
        <div className="flex w-72 items-center justify-between rounded-md border px-3 py-2 text-sm">
          <span>Anchored row content</span>
          <PopoverTrigger asChild>
            <button className="rounded-md border px-2 py-1 text-xs">
              Details
            </button>
          </PopoverTrigger>
        </div>
      </PopoverAnchor>
      <PopoverContent align="end">
        <p className="text-sm">
          The popover is anchored to the row instead of the trigger button.
        </p>
      </PopoverContent>
    </Popover>
  ),
};

function ControlledPopover() {
  const [open, setOpen] = useState(false);

  return (
    <div className="flex flex-col gap-2">
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <button className="rounded-md border px-3 py-1.5 text-sm">
            {open ? "Close" : "Open"} controlled popover
          </button>
        </PopoverTrigger>
        <PopoverContent>
          <p className="text-sm">
            Open state is owned by the parent component.
          </p>
        </PopoverContent>
      </Popover>
      <p className="text-muted-foreground text-xs">open: {String(open)}</p>
    </div>
  );
}

export const Controlled: Story = {
  render: () => <ControlledPopover />,
};
