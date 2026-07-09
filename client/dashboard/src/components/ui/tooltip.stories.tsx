import type { Meta, StoryObj } from "@storybook/react-vite";
import { Info } from "lucide-react";

import {
  SimpleTooltip,
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";

const meta: Meta<typeof Tooltip> = {
  title: "UI/Tooltip",
  component: Tooltip,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof Tooltip>;

export const Default: Story = {
  render: () => (
    <Tooltip>
      <TooltipTrigger asChild>
        <button className="rounded-md border px-3 py-1.5 text-sm">
          Hover me
        </button>
      </TooltipTrigger>
      <TooltipContent>Helpful context for this action</TooltipContent>
    </Tooltip>
  ),
};

export const WithIconTrigger: Story = {
  render: () => (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          aria-label="More information"
          className="text-muted-foreground rounded-full p-1 hover:bg-black/5"
        >
          <Info className="size-4" />
        </button>
      </TooltipTrigger>
      <TooltipContent>Additional details about this field</TooltipContent>
    </Tooltip>
  ),
};

export const Inverted: Story = {
  render: () => (
    <Tooltip>
      <TooltipTrigger asChild>
        <button className="rounded-md border px-3 py-1.5 text-sm">
          Inverted style
        </button>
      </TooltipTrigger>
      <TooltipContent inverted>Uses a light background instead</TooltipContent>
    </Tooltip>
  ),
};

export const Placement: Story = {
  render: () => (
    <div className="flex items-center gap-8 p-8">
      <Tooltip defaultOpen>
        <TooltipTrigger asChild>
          <button className="rounded-md border px-3 py-1.5 text-sm">Top</button>
        </TooltipTrigger>
        <TooltipContent side="top">Appears above</TooltipContent>
      </Tooltip>
      <Tooltip defaultOpen>
        <TooltipTrigger asChild>
          <button className="rounded-md border px-3 py-1.5 text-sm">
            Right
          </button>
        </TooltipTrigger>
        <TooltipContent side="right">Appears to the right</TooltipContent>
      </Tooltip>
      <Tooltip defaultOpen>
        <TooltipTrigger asChild>
          <button className="rounded-md border px-3 py-1.5 text-sm">
            Bottom
          </button>
        </TooltipTrigger>
        <TooltipContent side="bottom">Appears below</TooltipContent>
      </Tooltip>
    </div>
  ),
};

export const UsingSimpleTooltip: Story = {
  name: "SimpleTooltip helper",
  render: () => (
    <SimpleTooltip tooltip="Wraps children in a Tooltip + TooltipTrigger">
      <button className="rounded-md border px-3 py-1.5 text-sm">
        SimpleTooltip
      </button>
    </SimpleTooltip>
  ),
};

export const DisabledTrigger: Story = {
  render: () => (
    <Tooltip>
      <TooltipTrigger asChild>
        <span tabIndex={0} className="inline-flex">
          <button
            disabled
            className="cursor-not-allowed rounded-md border px-3 py-1.5 text-sm opacity-50"
          >
            Disabled action
          </button>
        </span>
      </TooltipTrigger>
      <TooltipContent>
        This action is unavailable until setup finishes
      </TooltipContent>
    </Tooltip>
  ),
};
