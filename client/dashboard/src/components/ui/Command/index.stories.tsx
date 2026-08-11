import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from ".";

const meta: Meta<typeof Command> = {
  title: "Design System/Command",
  component: Command,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Command>;

export const Default: Story = {
  render: () => (
    <Command className="w-96 border">
      <CommandInput placeholder="Search projects…" />
      <CommandList>
        <CommandEmpty>No results.</CommandEmpty>
        <CommandGroup heading="Projects">
          <CommandItem>Petstore</CommandItem>
          <CommandItem>Billing</CommandItem>
          <CommandItem>Internal tools</CommandItem>
        </CommandGroup>
      </CommandList>
    </Command>
  ),
};
