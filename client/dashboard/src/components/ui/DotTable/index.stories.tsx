import type { Meta, StoryObj } from "@storybook/react-vite";

import { DotTable } from ".";
import { DotRow } from "@/components/ui/DotRow";
import { Icon } from "@/components/ui/Icon";

const meta: Meta<typeof DotTable> = {
  title: "Design System/DotTable",
  component: DotTable,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof DotTable>;

export const Default: Story = {
  render: () => (
    <DotTable headers={[{ label: "Server" }, { label: "Tools" }]}>
      {["Petstore", "Billing"].map((name) => (
        <DotRow key={name} icon={<Icon name="server" className="size-5" />}>
          <td className="px-4 py-3 font-medium">{name}</td>
          <td className="text-muted-foreground px-4 py-3 text-sm">12 tools</td>
        </DotRow>
      ))}
    </DotTable>
  ),
};
