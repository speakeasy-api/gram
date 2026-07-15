import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";

const meta: Meta<typeof Sheet> = {
  title: "UI/Sheet",
  component: Sheet,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof Sheet>;

// Sheet only exposes Root/Content/Header/Footer/Title/Description — there is
// no SheetTrigger export, so triggering is done via controlled open state.
function TriggerableSheet() {
  const [open, setOpen] = useState(false);

  return (
    <>
      <Button variant="secondary" onClick={() => setOpen(true)}>
        View chat details
      </Button>
      <Sheet open={open} onOpenChange={setOpen}>
        <SheetContent>
          <SheetHeader>
            <SheetTitle>Chat session</SheetTitle>
            <SheetDescription>
              Trace details for this conversation.
            </SheetDescription>
          </SheetHeader>
          <div className="flex-1 px-4 text-sm">
            <p>Session ID: sess_8f21a9</p>
            <p>Duration: 4m 12s</p>
          </div>
          <SheetFooter>
            <Button variant="secondary" onClick={() => setOpen(false)}>
              Close
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>
    </>
  );
}

export const WithTrigger: Story = {
  render: () => <TriggerableSheet />,
};

export const DefaultOpen: Story = {
  render: () => (
    <Sheet defaultOpen>
      <SheetContent>
        <SheetHeader>
          <SheetTitle>Filter results</SheetTitle>
          <SheetDescription>
            Narrow the tool logs shown in the table below.
          </SheetDescription>
        </SheetHeader>
        <div className="flex-1 px-4 text-sm">
          <p>Server, user, agent, type, and date filters live here.</p>
        </div>
        <SheetFooter>
          <Button>Apply filters</Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  ),
};

export const LeftSide: Story = {
  render: () => (
    <Sheet defaultOpen>
      <SheetContent side="left">
        <SheetHeader>
          <SheetTitle>Navigation</SheetTitle>
          <SheetDescription>Slides in from the left edge.</SheetDescription>
        </SheetHeader>
      </SheetContent>
    </Sheet>
  ),
};

export const BottomSide: Story = {
  render: () => (
    <Sheet defaultOpen>
      <SheetContent side="bottom">
        <SheetHeader>
          <SheetTitle>Notifications</SheetTitle>
          <SheetDescription>Slides up from the bottom edge.</SheetDescription>
        </SheetHeader>
      </SheetContent>
    </Sheet>
  ),
};

export const WithoutCloseButton: Story = {
  render: () => (
    <Sheet defaultOpen>
      <SheetContent showCloseButton={false}>
        <SheetHeader>
          <SheetTitle>No close button</SheetTitle>
          <SheetDescription>
            showCloseButton is set to false, so dismissal must be driven by
            footer actions instead.
          </SheetDescription>
        </SheetHeader>
      </SheetContent>
    </Sheet>
  ),
};
