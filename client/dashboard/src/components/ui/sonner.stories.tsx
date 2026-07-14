import type { Meta, StoryObj } from "@storybook/react-vite";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Toaster } from "@/components/ui/sonner";

const meta: Meta<typeof Toaster> = {
  title: "UI/Toaster",
  component: Toaster,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof Toaster>;

export const Default: Story = {
  render: () => (
    <div>
      <Toaster />
      <Button
        onClick={() => {
          toast("Deployment started");
        }}
      >
        Show toast
      </Button>
    </div>
  ),
};

export const Success: Story = {
  render: () => (
    <div>
      <Toaster />
      <Button
        onClick={() => {
          toast.success("Changes saved");
        }}
      >
        Show success toast
      </Button>
    </div>
  ),
};

export const Error: Story = {
  render: () => (
    <div>
      <Toaster />
      <Button
        variant="destructive-primary"
        onClick={() => {
          toast.error("Failed to save changes");
        }}
      >
        Show error toast
      </Button>
    </div>
  ),
};

export const WithDescription: Story = {
  render: () => (
    <div>
      <Toaster />
      <Button
        onClick={() => {
          toast("Deployment queued", {
            description: "It may take a few minutes to complete.",
          });
        }}
      >
        Show toast with description
      </Button>
    </div>
  ),
};
