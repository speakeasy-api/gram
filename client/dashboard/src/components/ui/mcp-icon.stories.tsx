import type { Meta, StoryObj } from "@storybook/react-vite";
import { McpIcon } from "@/components/ui/mcp-icon";

/**
 * The file also defines `ExternalMcpIcon` (a tooltip-wrapped `McpIcon`), but it
 * is never `export`ed and has no other references in the codebase — it's dead
 * code, so there's nothing to story for it. Only `McpIcon` is a usable export.
 */
const meta: Meta<typeof McpIcon> = {
  title: "UI/McpIcon",
  component: McpIcon,
  parameters: {
    layout: "padded",
  },
};

export default meta;

type Story = StoryObj<typeof McpIcon>;

export const Default: Story = {
  args: {},
};

export const Sizes: Story = {
  render: () => (
    <div className="flex items-center gap-4">
      <McpIcon size={16} />
      <McpIcon size={24} />
      <McpIcon size={32} />
      <McpIcon size={48} />
    </div>
  ),
};

export const StrokeWidths: Story = {
  render: () => (
    <div className="flex items-center gap-4">
      <McpIcon size={32} strokeWidth={1} />
      <McpIcon size={32} strokeWidth={2} />
      <McpIcon size={32} strokeWidth={3} />
    </div>
  ),
};

export const CustomColor: Story = {
  render: () => (
    <div className="flex items-center gap-4">
      <McpIcon size={32} color="#1DA1F2" />
      <McpIcon size={32} color="var(--color-destructive)" />
      <McpIcon size={32} className="text-success" />
    </div>
  ),
};

export const InButton: Story = {
  render: () => (
    <button
      type="button"
      className="border-border bg-card hover:bg-muted flex items-center gap-2 rounded-md border px-3 py-2 text-sm"
    >
      <McpIcon size={16} />
      Connect MCP server
    </button>
  ),
};
