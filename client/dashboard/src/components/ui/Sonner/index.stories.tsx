import type { Meta, StoryObj } from "@storybook/react-vite";
import { toast } from "sonner";

import { Toaster } from ".";
import { Button } from "@/components/ui/Button";

const meta: Meta<typeof Toaster> = {
  title: "Design System/Sonner",
  component: Toaster,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Toaster>;

export const Default: Story = {
  render: () => (
    <>
      <Toaster />
      <div className="flex gap-2">
        <Button
          onClick={() => {
            toast.success("Deployment published");
          }}
        >
          Success
        </Button>
        <Button
          variant="destructive-secondary"
          onClick={() => {
            toast.error("Could not reach the server");
          }}
        >
          Error
        </Button>
      </div>
    </>
  ),
};
