import type { Meta, StoryObj } from "@storybook/react-vite";

import { Popover, PopoverContent, PopoverTrigger } from ".";
import { Button } from "@/components/ui/Button";

const meta: Meta<typeof Popover> = {
  title: "Design System/Popover",
  component: Popover,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Popover>;

export const Default: Story = {
  render: () => (
    <Popover>
      <PopoverTrigger asChild>
        <Button variant="secondary">Filters</Button>
      </PopoverTrigger>
      <PopoverContent className="text-sm">
        Narrow the list by environment, status or owner.
      </PopoverContent>
    </Popover>
  ),
};
