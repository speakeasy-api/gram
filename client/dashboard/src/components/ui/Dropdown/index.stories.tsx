import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from ".";
import { Button } from "@/components/ui/Button";

const meta: Meta<typeof DropdownMenu> = {
  title: "Design System/Dropdown",
  component: DropdownMenu,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof DropdownMenu>;

export const Default: Story = {
  render: () => (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="secondary">Open</Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="max-w-64">
        <DropdownMenuLabel>
          <div className="flex flex-col gap-1">
            <div>Jane Smith</div>
            <div className="text-body text-sm font-normal">
              jane@example.com
            </div>
          </div>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem>Account settings</DropdownMenuItem>
        <DropdownMenuItem>Switch organisation</DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem>Sign out</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  ),
};
