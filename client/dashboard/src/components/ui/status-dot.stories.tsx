import type { Meta, StoryObj } from "@storybook/react-vite";

import { StatusDot } from "@/components/ui/status-dot";

const meta: Meta<typeof StatusDot> = {
  title: "UI/StatusDot",
  component: StatusDot,
  tags: ["autodocs"],
};

export default meta;

type Story = StoryObj<typeof StatusDot>;

export const Success: Story = {
  args: { tone: "success", label: "Active" },
};

export const Warning: Story = {
  args: { tone: "warning", label: "Degraded" },
};

export const Destructive: Story = {
  args: { tone: "destructive", label: "Revoked" },
};

export const Information: Story = {
  args: { tone: "information", label: "Syncing" },
};

export const Neutral: Story = {
  args: { tone: "neutral", label: "Expired" },
};

export const Pulsing: Story = {
  args: { tone: "information", label: "Deploying", pulse: true },
};

export const DotOnly: Story = {
  args: { tone: "success" },
};

export const Small: Story = {
  args: { tone: "success", label: "Active", size: "sm" },
};

export const AllTones: Story = {
  render: () => (
    <div className="flex flex-col gap-2">
      <StatusDot tone="success" label="Active" />
      <StatusDot tone="warning" label="Degraded" />
      <StatusDot tone="destructive" label="Revoked" />
      <StatusDot tone="information" label="Syncing" pulse />
      <StatusDot tone="neutral" label="Expired" />
    </div>
  ),
};
