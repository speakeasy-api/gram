import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
} from "./dropdown-menu";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { Button } from "./button";
import { Stack } from "./stack";
import { Icon } from "./icon";

const meta: Meta<typeof DropdownMenu> = {
  title: "UI/Dropdown",
  component: DropdownMenu,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: {
    children: [
      <DropdownMenuTrigger asChild key="trigger">
        <Button>Open</Button>
      </DropdownMenuTrigger>,
      <DropdownMenuContent align="start" className="max-w-64" key="content">
        <DropdownMenuLabel>
          <Stack direction="vertical" gap={1}>
            <div>Jane Smith</div>
            <div className="text-sm font-normal text-body">
              jane@example.com
            </div>
          </Stack>
        </DropdownMenuLabel>
        <DropdownMenuSeparator className="my-2" />
        <DropdownMenuItem className="cursor-pointer">
          <Stack direction="horizontal" gap={2} align="center">
            <Icon name="log-out" />
            <span>Log out</span>
          </Stack>
        </DropdownMenuItem>
      </DropdownMenuContent>,
    ],
  },
};
