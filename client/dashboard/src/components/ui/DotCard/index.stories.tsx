import type { Meta, StoryObj } from "@storybook/react-vite";

import { DotCard } from ".";
import { Icon } from "@/components/ui/Icon";

const meta: Meta<typeof DotCard> = {
  title: "Design System/DotCard",
  component: DotCard,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof DotCard>;

export const Default: Story = {
  render: () => (
    <div className="w-[32rem]">
      <DotCard icon={<Icon name="server" className="size-6" />}>
        <div className="p-4">
          <div className="font-medium">Petstore</div>
          <div className="text-muted-foreground text-sm">
            12 tools · updated 2 days ago
          </div>
        </div>
      </DotCard>
    </div>
  ),
};
