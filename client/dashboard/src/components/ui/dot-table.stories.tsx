import type { Meta, StoryObj } from "@storybook/react-vite";
import { faker } from "@faker-js/faker";
import { Database, GitBranch, MessageSquare } from "lucide-react";
import { DotTable } from "@/components/ui/dot-table";
import { DotRow } from "@/components/ui/dot-row";
import { Badge, Button } from "@/components/ui/moonshine";
import { Type } from "@/components/ui/type";

faker.seed(23);

const meta: Meta<typeof DotTable> = {
  title: "UI/DotTable",
  component: DotTable,
  parameters: {
    layout: "padded",
  },
};

export default meta;

type Story = StoryObj<typeof DotTable>;

interface ServerRow {
  id: string;
  name: string;
  version: string;
  description: string;
  toolCount: number;
  Icon: typeof GitBranch;
}

function makeRows(count: number): ServerRow[] {
  const icons = [GitBranch, MessageSquare, Database];
  return Array.from({ length: count }, (_, i) => ({
    id: faker.string.uuid(),
    name: faker.company.name(),
    version: `v${faker.number.int({ min: 1, max: 9 })}.${faker.number.int({ min: 0, max: 9 })}`,
    description: faker.lorem.sentence(10),
    toolCount: faker.number.int({ min: 0, max: 40 }),
    Icon: icons[i % icons.length]!,
  }));
}

export const Default: Story = {
  render: () => {
    const rows = makeRows(5);
    return (
      <DotTable
        headers={[
          { label: "Name" },
          { label: "Version" },
          { label: "Description" },
          { label: "Tools" },
          { label: "" },
        ]}
      >
        {rows.map((row) => (
          <DotRow key={row.id} icon={<row.Icon className="size-5" />}>
            <td className="px-3 py-3">
              <Type variant="subheading" as="div" className="truncate text-sm">
                {row.name}
              </Type>
            </td>
            <td className="px-3 py-3">
              <Type small muted>
                {row.version}
              </Type>
            </td>
            <td className="max-w-xs px-3 py-3">
              <Type small muted className="block truncate">
                {row.description}
              </Type>
            </td>
            <td className="px-3 py-3">
              <Badge variant="neutral">
                <Badge.Text>{row.toolCount} tools</Badge.Text>
              </Badge>
            </td>
            <td className="px-3 py-3">
              <Button variant="secondary" size="sm">
                <Button.Text>View</Button.Text>
              </Button>
            </td>
          </DotRow>
        ))}
      </DotTable>
    );
  },
};

export const WithSelection: Story = {
  render: () => {
    const rows = makeRows(4);
    return (
      <DotTable
        headers={[{ label: "Name" }, { label: "Version" }, { label: "Tools" }]}
        selectionHeader={
          <input type="checkbox" aria-label="Select all servers" />
        }
      >
        {rows.map((row, i) => (
          <DotRow key={row.id} icon={<row.Icon className="size-5" />}>
            <td className="w-10 px-3 py-3">
              <input
                type="checkbox"
                defaultChecked={i === 0}
                aria-label={`Select ${row.name}`}
              />
            </td>
            <td className="px-3 py-3">
              <Type variant="subheading" as="div" className="truncate text-sm">
                {row.name}
              </Type>
            </td>
            <td className="px-3 py-3">
              <Type small muted>
                {row.version}
              </Type>
            </td>
            <td className="px-3 py-3">
              <Badge variant="neutral">
                <Badge.Text>{row.toolCount} tools</Badge.Text>
              </Badge>
            </td>
          </DotRow>
        ))}
      </DotTable>
    );
  },
};

export const Empty: Story = {
  render: () => (
    <DotTable
      headers={[{ label: "Name" }, { label: "Version" }, { label: "Tools" }]}
    >
      <tr>
        <td colSpan={4} className="px-3 py-8 text-center">
          <Type small muted>
            No matching servers
          </Type>
        </td>
      </tr>
    </DotTable>
  ),
};
