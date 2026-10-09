import type { Meta, StoryObj } from "@storybook/react-vite";

import { ConfirmDialog } from ".";

const meta: Meta<typeof ConfirmDialog> = {
  title: "Design System/ConfirmDialog",
  component: ConfirmDialog,
  tags: ["autodocs"],
  args: {
    open: true,
    onOpenChange: () => {},
    onConfirm: () => {},
    title: "Delete this server?",
    description: "People using it lose access to its tools straight away.",
    confirmLabel: "Delete server",
  },
};

export default meta;
type Story = StoryObj<typeof ConfirmDialog>;

export const Default: Story = {};

export const Pending: Story = {
  args: { isPending: true, pendingLabel: "Deleting…" },
};

export const WithImpact: Story = {
  args: {
    impact: {
      summary: "3 MCP servers use this sign-in. They will stop working.",
      mcpServerNames: ["Linear", "Petstore", "Internal tools"],
    },
  },
};

export const WithError: Story = {
  args: {
    error: "This server is still in use. Remove it from its plugins first.",
  },
};

export const SafeAction: Story = {
  args: {
    title: "Activate this key?",
    description: "New tokens are signed with this key from now on.",
    confirmLabel: "Activate",
    confirmVariant: "primary",
  },
};
