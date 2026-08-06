import type { Meta, StoryObj } from "@storybook/react-vite";

import { SimpleTooltip, Tooltip, TooltipContent, TooltipTrigger } from ".";
import { Button } from "@/components/ui/Button";

const meta: Meta<typeof Tooltip> = {
  title: "Design System/Tooltip",
  component: Tooltip,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Tooltip>;

export const Default: Story = {
  render: () => (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button variant="secondary">Hover me</Button>
      </TooltipTrigger>
      <TooltipContent>Publishes to every attached environment.</TooltipContent>
    </Tooltip>
  ),
};

export const Sides: Story = {
  render: () => (
    <div className="flex gap-8 p-16">
      {(["top", "right", "bottom", "left"] as const).map((side) => (
        <Tooltip key={side}>
          <TooltipTrigger asChild>
            <Button variant="secondary">{side}</Button>
          </TooltipTrigger>
          <TooltipContent side={side}>Opens on the {side}</TooltipContent>
        </Tooltip>
      ))}
    </div>
  ),
};

export const Inverted: Story = {
  render: () => (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button variant="secondary">Inverted surface</Button>
      </TooltipTrigger>
      <TooltipContent inverted>
        Light surface in light mode, for dense inline help.
      </TooltipContent>
    </Tooltip>
  ),
};

export const Simple: Story = {
  render: () => (
    <SimpleTooltip tooltip="Trigger and content in one call">
      <Button variant="secondary">SimpleTooltip</Button>
    </SimpleTooltip>
  ),
};
