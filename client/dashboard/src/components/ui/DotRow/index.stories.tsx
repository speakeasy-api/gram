import type { Meta, StoryObj } from "@storybook/react-vite";

import { DotRow } from ".";
import { DotTable } from "@/components/ui/DotTable";
import { Icon } from "@/components/ui/Icon";

const meta: Meta<typeof DotRow> = {
  title: "Design System/DotRow",
  component: DotRow,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof DotRow>;

export const Linked: Story = {
  render: () => (
    <DotTable headers={[{ label: "Server" }, { label: "Tools" }]}>
      <DotRow
        icon={<Icon name="server" className="size-5" />}
        href="#petstore"
        ariaLabel="Open Petstore"
      >
        <td className="px-4 py-3 font-medium">Petstore</td>
        <td className="text-muted-foreground px-4 py-3 text-sm">12 tools</td>
      </DotRow>
    </DotTable>
  ),
};
