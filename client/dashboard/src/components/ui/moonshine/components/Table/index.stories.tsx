import type { Meta, StoryObj } from "@storybook/react-vite";
import {
  Table,
  type Column,
  type Group,
  type SortDescriptor,
  type TableProps,
} from ".";
import { sortTableData } from "./sorting";
import { faker } from "@faker-js/faker";
import { useState } from "react";
import {
  SupportedLanguage,
  supportedLanguages,
} from "@/components/ui/moonshine/types";
import { formatDistance } from "date-fns";
import { Icon } from "../Icon";

faker.seed(123);

const meta: Meta<typeof Table> = {
  title: "Moonshine/Table",
  component: Table,
  decorators: [
    (Story) => (
      <div className="mx-auto my-10">
        <Story />
      </div>
    ),
  ],
};

export default meta;

interface SDK {
  name: string;
  org: string;
  version: string;
  lastGeneratedAt: Date;
  description: string;
  language: SupportedLanguage;
  githubRepoUrl: string;
}

function generateSDKs(count: number): SDK[] {
  return Array.from({ length: count }, (_, i) => ({
    name: faker.lorem.slug({ min: 1, max: 1 }),
    org: faker.lorem.slug({ min: 1, max: 1 }),
    version: `v${faker.number.int({ min: 1, max: 10 })}.${faker.number.int({
      min: 0,
      max: 9,
    })}.${faker.number.int({ min: 0, max: 9 })}`,
    lastGeneratedAt: faker.date.recent({ days: i + 1 }),
    description: faker.commerce.productDescription(),
    language: faker.helpers.arrayElement(supportedLanguages),
    githubRepoUrl: faker.internet.url(),
  }));
}

const sdk: SDK[] = generateSDKs(5);

const defaultArgs: ListTableProps = {
  columns: [
    {
      key: "language",
      header: undefined,
      width: "0.5fr",
      render: (row) => (
        <span className="font-mono text-xs">{row.language}</span>
      ),
    },
    {
      key: "name",
      header: "SDK",
      width: "1fr",
      render: (row) => (
        <div className="flex flex-row items-center gap-1 text-body">
          <span>
            {row.org}/{row.name}
          </span>
        </div>
      ),
    },
    {
      key: "version",
      header: "Version",
      width: "0.75fr",
      render: (row) => (
        <div className="text-body">{row.version.toString()}</div>
      ),
    },
    {
      key: "lastGeneratedAt",
      header: "Last Generated",
      width: "1.25fr",
      render: (row) => (
        <div className="text-body">
          {formatDistance(row.lastGeneratedAt, new Date(), { addSuffix: true })}
        </div>
      ),
    },
    {
      key: "githubRepoUrl",
      header: "GitHub Repo",
      width: "4fr",
      render: (row) => (
        <div>
          <a href="#" className="text-body">
            github.com/{row.name}/{row.org}
          </a>
        </div>
      ),
    },
  ] as Column<SDK>[],
  data: sdk,
  rowKey: (row: SDK) => row.name,
  onRowClick: (row: SDK) => alert("Clicked on " + row.name),
  hasMore: true,
};

type ListTableProps = TableProps<SDK> & { data: SDK[] };

const sortableColumns: Column<SDK>[] = defaultArgs.columns.map(
  (column): Column<SDK> => {
    if (column.key === "name") {
      return {
        ...column,
        id: "sdk-name",
        sortable: true,
        sortValue: (row) => `${row.org}/${row.name}`,
      };
    }

    if (column.key === "version") {
      return {
        ...column,
        id: "sdk-version",
        sortable: true,
        sortValue: (row) => row.version,
      };
    }

    if (column.key === "lastGeneratedAt") {
      return {
        ...column,
        id: "sdk-last-generated-at",
        sortable: true,
        sortLabel: "Last Generated",
        sortValue: (row) => row.lastGeneratedAt,
      };
    }

    return column;
  },
);

const TableWithState = (args: ListTableProps) => {
  const [data, setData] = useState<SDK[]>(args.data);
  return (
    <Table
      {...args}
      data={data}
      onLoadMore={async () => {
        setData((prev) => [...prev, ...generateSDKs(2)]);
      }}
    />
  );
};

const SortableTableWithState = (args: ListTableProps) => {
  const [data, setData] = useState<SDK[]>(args.data);
  const [sort, setSort] = useState<SortDescriptor | null>(null);

  const sortedData = sortTableData(data, args.columns, sort) as SDK[];

  return (
    <Table
      {...args}
      data={sortedData}
      sort={sort}
      onSortChange={setSort}
      onLoadMore={async () => {
        setData((prev) => [...prev, ...generateSDKs(2)]);
      }}
    />
  );
};

export const Default: StoryObj<ListTableProps> = {
  args: defaultArgs,
  render: (args) => <TableWithState {...args} />,
};

export const Sortable: StoryObj<ListTableProps> = {
  args: {
    ...defaultArgs,
    columns: sortableColumns,
    hasMore: false,
  },
  render: (args) => <SortableTableWithState {...args} />,
};

export const Condensed: StoryObj<ListTableProps> = {
  args: {
    ...defaultArgs,
    cellPadding: "condensed",
  },
  render: (args) => <TableWithState {...args} />,
};

export const Spacious: StoryObj<ListTableProps> = {
  args: {
    ...defaultArgs,
    cellPadding: "spacious",
  },
  render: (args) => <TableWithState {...args} />,
};

type GroupedTableProps = TableProps<SDK> & { data: Group<SDK>[] };

const GroupedTableWithState = (args: GroupedTableProps) => {
  const [data, setData] = useState<Group<SDK>[]>(args.data);
  return (
    <Table
      {...args}
      data={data}
      onLoadMore={async () => {
        setData((prev) => [
          ...prev,
          { key: "my-new-source", items: generateSDKs(2), count: 2 },
        ]);
      }}
    />
  );
};

export const Grouped: StoryObj<GroupedTableProps> = {
  args: {
    ...defaultArgs,
    data: [
      { key: "my-source", items: sdk.slice(0, 2), count: 2 },
      { key: "my-other-source", items: sdk.slice(2, 4), count: 2 },
    ],
    renderGroupHeader: (group) => (
      <div className="flex w-full flex-row items-center gap-2 border-b bg-zinc-50 px-4 py-2 font-semibold text-body dark:bg-zinc-900">
        <Icon name="code" />
        {group.key} ({group.count as number} SDKs total)
      </div>
    ),
  },
  render: (args) => <GroupedTableWithState {...args} />,
};

export const WithLotsOfColumns: StoryObj<ListTableProps> = {
  args: {
    ...defaultArgs,
    columns: defaultArgs.columns.concat(defaultArgs.columns),
  },
  render: (args) => <TableWithState {...args} />,
};

export const AllAutoColumnWidths: StoryObj<ListTableProps> = {
  args: {
    ...defaultArgs,
    columns: defaultArgs.columns.map((col) => {
      return {
        ...col,
        width: "auto",
      };
    }),
  },
  render: (args) => <TableWithState {...args} />,
};

export const MixedAutoColumnWidths: StoryObj<ListTableProps> = {
  args: {
    ...defaultArgs,
    columns: defaultArgs.columns.map((col, index) => {
      return {
        ...col,
        width: index == defaultArgs.columns.length - 1 ? "1fr" : "auto",
      };
    }),
  },
  render: (args) => <TableWithState {...args} />,
};

export const Expandable: StoryObj<ListTableProps> = {
  args: {
    ...defaultArgs,
    onRowClick: undefined,
    renderExpandedContent: (row) =>
      row.language === "python" ? null : (
        <div className="bg-green-500 p-4">MY CUSTOM COMPONENT</div>
      ),
  },
  render: (args) => <TableWithState {...args} />,
};

export const ExpandableWithDefaultExpanded: StoryObj<ListTableProps> = {
  args: {
    ...Expandable.args,
    data: defaultArgs.data.map((row) => ({
      ...row,
      defaultExpanded: true,
    })),
  },
  render: (args) => <TableWithState {...args} />,
};

export const ExpandableWithSubtable: StoryObj<ListTableProps> = {
  args: {
    ...defaultArgs,
    onRowClick: undefined,
    renderExpandedContent: () => (
      <TableWithState
        {...(Expandable.args as ListTableProps)}
        hideHeader={true}
        hasMore={false}
      />
    ),
  },
  render: (args) => <TableWithState {...args} />,
};

export const Customized: StoryObj<ListTableProps> = {
  args: {
    ...defaultArgs,
    cellPadding: "condensed",
  },
  render: (args) => (
    <Table {...args}>
      <Table.Header columns={args.columns} />
      <p className={"[grid-column:1/-1] bg-green-500 p-4"}>
        MY CUSTOM COMPONENT
      </p>
      <Table.Body {...args} hasMore={false} />
    </Table>
  ),
};

export const CustomizedEvenMore: StoryObj<ListTableProps> = {
  args: {
    ...defaultArgs,
    cellPadding: "condensed",
  },
  render: (args) => (
    <Table {...args}>
      <Table.Header columns={args.columns} />
      <p className={"[grid-column:1/-1] bg-green-700 p-4"}>
        MY CUSTOM COMPONENT
      </p>
      <Table.Body>
        <Table.Row row={args.data[0]!} columns={args.columns} />
        <p className={"[grid-column:1/-1] bg-yellow-700 p-4"}>
          ANOTHER CUSTOM COMPONENT
        </p>
        <Table.Row row={args.data[1]!} columns={args.columns} />
        <Table.Row>
          <Table.Cell row={args.data[2]!} column={args.columns[0]!} />
          <Table.Cell row={args.data[2]!} column={args.columns[1]!} />
          <Table.Cell>
            <p className={"bg-cyan-700 p-1"}>CUSTOM CELL</p>
          </Table.Cell>
          <Table.Cell row={args.data[2]!} column={args.columns[3]!} />
        </Table.Row>
      </Table.Body>
    </Table>
  ),
};
