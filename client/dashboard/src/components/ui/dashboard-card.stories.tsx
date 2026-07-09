import type { Meta, StoryObj } from "@storybook/react-vite";
import { faker } from "@faker-js/faker";
import { Link } from "react-router";
import { DashboardCard } from "@/components/ui/dashboard-card";
import { Button } from "@/components/ui/moonshine";

faker.seed(17);

const meta: Meta<typeof DashboardCard> = {
  title: "UI/DashboardCard",
  component: DashboardCard,
  parameters: {
    layout: "padded",
  },
};

export default meta;

type Story = StoryObj<typeof DashboardCard>;

function RankedList({ items }: { items: { label: string; value: number }[] }) {
  const max = Math.max(...items.map((i) => i.value), 1);
  return (
    <ul className="divide-border divide-y">
      {items.map((item) => (
        <li
          key={item.label}
          className="flex items-center gap-3 py-2.5 first:pt-0 last:pb-0"
        >
          <div className="min-w-0 flex-1">
            <p className="truncate text-sm font-medium">{item.label}</p>
            <div className="bg-muted mt-1 h-1.5 w-full overflow-hidden rounded-full">
              <div
                className="bg-primary h-full rounded-full"
                style={{ width: `${(item.value / max) * 100}%` }}
              />
            </div>
          </div>
          <span className="text-muted-foreground shrink-0 text-xs tabular-nums">
            {item.value.toLocaleString()}
          </span>
        </li>
      ))}
    </ul>
  );
}

const topServers = Array.from({ length: 5 }, () => ({
  label: faker.company.name(),
  value: faker.number.int({ min: 20, max: 900 }),
})).sort((a, b) => b.value - a.value);

export const Default: Story = {
  render: () => (
    <div className="max-w-md">
      <DashboardCard title="Top Servers">
        <RankedList items={topServers} />
      </DashboardCard>
    </div>
  ),
};

export const WithTooltip: Story = {
  render: () => (
    <div className="max-w-md">
      <DashboardCard
        title="Tool Calls"
        tooltip="Total tool invocations recorded across all servers and sources in the selected period."
      >
        <RankedList items={topServers} />
      </DashboardCard>
    </div>
  ),
};

export const WithAction: Story = {
  render: () => (
    <div className="max-w-md">
      <DashboardCard
        title="Top Users"
        tooltip="Employees ranked by total token consumption in the selected period."
        action={
          <Link
            to="#"
            className="text-muted-foreground hover:text-foreground flex items-center gap-0.5 text-xs no-underline"
          >
            View all
          </Link>
        }
      >
        <RankedList items={topServers} />
      </DashboardCard>
    </div>
  ),
};

export const EmptyState: Story = {
  render: () => (
    <div className="max-w-md">
      <DashboardCard
        title="Top Tools by Failure Rate"
        tooltip="Tools with the highest share of failed calls in the selected period."
        action={<Button size="sm">View all</Button>}
      >
        <p className="text-muted-foreground text-sm">
          No tool failures recorded
        </p>
      </DashboardCard>
    </div>
  ),
};

export const Grid: Story = {
  render: () => (
    <div className="grid grid-cols-1 gap-6 md:grid-cols-2">
      <DashboardCard
        title="Top Servers"
        tooltip="Servers ranked by tool calls served."
      >
        <RankedList items={topServers} />
      </DashboardCard>
      <DashboardCard
        title="Most Used Agents"
        tooltip="Agents ranked by activity volume."
      >
        <RankedList
          items={Array.from({ length: 5 }, () => ({
            label: faker.helpers.arrayElement([
              "Claude Code",
              "Cursor",
              "Codex",
              "Windsurf",
              "Copilot",
            ]),
            value: faker.number.int({ min: 10, max: 500 }),
          }))}
        />
      </DashboardCard>
    </div>
  ),
};
