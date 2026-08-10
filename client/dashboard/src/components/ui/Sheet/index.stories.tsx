import type { Meta, StoryObj } from "@storybook/react-vite";

import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from ".";
import { Button } from "@/components/ui/Button";

const meta: Meta<typeof Sheet> = {
  title: "Design System/Sheet",
  component: Sheet,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Sheet>;

export const Default: Story = {
  render: () => (
    <Sheet defaultOpen>
      <SheetContent>
        <SheetHeader>
          <SheetTitle>Edit server</SheetTitle>
          <SheetDescription>
            Changes apply to the next deployment.
          </SheetDescription>
        </SheetHeader>
        <SheetFooter>
          <Button>Save</Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  ),
};
