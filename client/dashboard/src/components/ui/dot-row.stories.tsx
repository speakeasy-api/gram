import type { Meta, StoryObj } from "@storybook/react-vite";
import { faker } from "@faker-js/faker";
import { Database, GitBranch, MessageSquare } from "lucide-react";
import { DotRow } from "@/components/ui/dot-row";
import { Badge } from "@/components/ui/badge";
import { Type } from "@/components/ui/type";

faker.seed(11);

/**
 * `DotRow` renders a `<tr>` and must live inside a `<table>` / `<tbody>` — the
 * stories wrap it accordingly (mirrors how `DotTable` uses it in the app).
 */
const meta: Meta<typeof DotRow> = {
  title: "UI/DotRow",
  component: DotRow,
  parameters: {
    layout: "padded",
  },
};

export default meta;

type Story = StoryObj<typeof DotRow>;

interface ServerRow {
  name: string;
  version: string;
  description: string;
  Icon: typeof GitBranch;
}

function makeRows(count: number): ServerRow[] {
  const icons = [GitBranch, MessageSquare, Database];
  return Array.from({ length: count }, (_, i) => ({
    name: faker.company.name(),
    version: `v${faker.number.int({ min: 1, max: 9 })}.${faker.number.int({ min: 0, max: 9 })}`,
    description: faker.lorem.sentence(10),
    Icon: icons[i % icons.length]!,
  }));
}

export const Default: Story = {
  render: () => {
    const rows = makeRows(3);
    return (
      <table className="w-full text-sm">
        <tbody>
          {rows.map((row) => (
            <DotRow key={row.name} icon={<row.Icon className="size-5" />}>
              <td className="px-3 py-3">
                <Type
                  variant="subheading"
                  as="div"
                  className="truncate text-sm"
                >
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
            </DotRow>
          ))}
        </tbody>
      </table>
    );
  },
};

export const WithLink: Story = {
  render: () => {
    const rows = makeRows(2);
    return (
      <table className="w-full text-sm">
        <tbody>
          {rows.map((row) => (
            <DotRow
              key={row.name}
              icon={<row.Icon className="size-5" />}
              href="#"
              ariaLabel={`View ${row.name}`}
            >
              <td className="px-3 py-3">
                <Type
                  variant="subheading"
                  as="div"
                  className="truncate text-sm"
                >
                  {row.name}
                </Type>
              </td>
              <td className="px-3 py-3">
                <Type small muted>
                  {row.version}
                </Type>
              </td>
            </DotRow>
          ))}
        </tbody>
      </table>
    );
  },
};

export const WithStatusBadge: Story = {
  render: () => {
    const rows = makeRows(3);
    return (
      <table className="w-full text-sm">
        <tbody>
          {rows.map((row, i) => (
            <DotRow
              key={row.name}
              icon={<row.Icon className="size-5" />}
              onClick={() => alert(`Selected ${row.name}`)}
            >
              <td className="px-3 py-3">
                <div className="flex items-center gap-2">
                  <Type
                    variant="subheading"
                    as="div"
                    className="truncate text-sm"
                  >
                    {row.name}
                  </Type>
                  {i === 0 && (
                    <Badge variant="success">
                      <Badge.Text>Added</Badge.Text>
                    </Badge>
                  )}
                </div>
              </td>
              <td className="px-3 py-3">
                <Type small muted>
                  {row.description}
                </Type>
              </td>
            </DotRow>
          ))}
        </tbody>
      </table>
    );
  },
};
