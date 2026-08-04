import type { Meta, StoryObj } from "@storybook/react-vite";

import { DashboardCard } from ".";
import { Button } from "@/components/ui/Button";

const meta: Meta<typeof DashboardCard> = {
  title: "Design System/DashboardCard",
  component: DashboardCard,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof DashboardCard>;

export const Default: Story = {
  render: () => (
    <div className="w-[32rem]">
      <DashboardCard
        title="Tool calls"
        tooltip="Calls across every MCP server in this project."
        action={
          <Button variant="tertiary" size="xs">
            View all
          </Button>
        }
      >
        <div className="text-3xl font-semibold tabular-nums">12,480</div>
      </DashboardCard>
    </div>
  ),
};
