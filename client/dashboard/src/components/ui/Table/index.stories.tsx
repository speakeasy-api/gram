import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";

import { Table, type Column, type SortDescriptor } from ".";
import { sortTableData } from "@/components/ui/Table/sorting";
import { Badge } from "@/components/ui/Badge";

const meta: Meta<typeof Table> = {
  title: "Design System/Table",
  component: Table,
  tags: ["autodocs"],
};

export default meta;
type Story = StoryObj<typeof Table>;

type Server = { id: string; name: string; tools: number; status: string };

const SERVERS: Server[] = [
  { id: "1", name: "Petstore", tools: 12, status: "live" },
  { id: "2", name: "Billing", tools: 5, status: "draft" },
  { id: "3", name: "Internal tools", tools: 31, status: "live" },
];

const columns: Column<Server>[] = [
  {
    key: "name",
    header: "Server",
    render: (row) => row.name,
    sortable: true,
    sortValue: (row) => row.name,
  },
  {
    key: "tools",
    header: "Tools",
    render: (row) => row.tools,
    sortable: true,
    sortValue: (row) => row.tools,
  },
  {
    key: "status",
    header: "Status",
    render: (row) => (
      <Badge variant={row.status === "live" ? "success" : "neutral"}>
        {row.status}
      </Badge>
    ),
  },
];

export const Default: Story = {
  render: () => (
    <Table columns={columns} data={SERVERS} rowKey={(row) => row.id} />
  ),
};

export const Sortable: Story = {
  render: function Render() {
    const [sort, setSort] = useState<SortDescriptor | null>({
      id: "tools",
      direction: "desc",
    });

    return (
      <Table
        columns={columns}
        data={sortTableData(SERVERS, columns, sort)}
        rowKey={(row) => row.id}
        sort={sort}
        onSortChange={setSort}
      />
    );
  },
};

export const Empty: Story = {
  render: () => (
    <Table
      columns={columns}
      data={[]}
      rowKey={(row) => row.id}
      noResultsMessage="No servers yet."
    />
  ),
};
