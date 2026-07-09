import type { Meta, StoryObj } from "@storybook/react-vite";
import {
  BarChart3,
  FileText,
  LogOut,
  Settings,
  Shield,
  User,
} from "lucide-react";
import { useState } from "react";

import { Button } from "@/components/ui/moonshine";
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from "@/components/ui/command";

const meta: Meta<typeof Command> = {
  title: "UI/Command",
  component: Command,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof Command>;

function PaletteBody() {
  return (
    <>
      <CommandInput placeholder="Search for a command..." />
      <CommandList>
        <CommandEmpty>No results found.</CommandEmpty>
        <CommandGroup heading="Navigate">
          <CommandItem>
            <BarChart3 />
            Go to Insights
          </CommandItem>
          <CommandItem>
            <FileText />
            Go to Logs
          </CommandItem>
          <CommandItem>
            <Shield />
            Go to Security
          </CommandItem>
        </CommandGroup>
        <CommandSeparator />
        <CommandGroup heading="Account">
          <CommandItem>
            <User />
            Profile settings
          </CommandItem>
          <CommandItem>
            <Settings />
            Project settings
          </CommandItem>
          <CommandItem>
            <LogOut />
            Log out
          </CommandItem>
        </CommandGroup>
      </CommandList>
    </>
  );
}

export const Default: Story = {
  render: () => (
    <div className="w-[400px] rounded-lg border shadow-md">
      <Command>
        <PaletteBody />
      </Command>
    </div>
  ),
};

export const DialogDefaultOpen: Story = {
  render: () => (
    <CommandDialog
      defaultOpen
      title="Command Palette"
      description="Search for a command to run"
    >
      <PaletteBody />
    </CommandDialog>
  ),
};

function TriggerableCommandDialog() {
  const [open, setOpen] = useState(false);

  return (
    <>
      <Button variant="secondary" onClick={() => setOpen(true)}>
        <Button.Text>Open command palette</Button.Text>
        <Button.RightIcon>
          <kbd className="bg-muted text-muted-foreground ml-2 rounded border px-1.5 py-0.5 text-xs">
            ⌘K
          </kbd>
        </Button.RightIcon>
      </Button>
      <CommandDialog open={open} onOpenChange={setOpen}>
        <PaletteBody />
      </CommandDialog>
    </>
  );
}

export const WithTrigger: Story = {
  render: () => <TriggerableCommandDialog />,
};

export const EmptyState: Story = {
  render: () => (
    <div className="w-[400px] rounded-lg border shadow-md">
      <Command>
        <CommandInput placeholder="Try typing something with no matches..." />
        <CommandList>
          <CommandEmpty>No results found.</CommandEmpty>
        </CommandList>
      </Command>
    </div>
  ),
};
