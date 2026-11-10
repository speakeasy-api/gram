# Tables

Use the design system `Table` from `@/components/ui/Table` for dashboard tables. Do **not** add new shadcn table wrappers or hand-roll table styling with raw `<table>` markup when `Table` can express the UI. If you find a lingering legacy table pattern, migrate it when touched.

```tsx
import { Column, Table } from "@/components/ui/Table";
```

For normal data tables, prefer the declarative `columns` / `data` / `rowKey` API. Define `Column<T>[]` near the component so render functions stay typed, use `render` for rich cells, and use `width` for stable layouts instead of ad hoc cell class widths.

```tsx
const columns: Column<Role>[] = [
  {
    key: "name",
    header: "Name",
    width: "180px",
    render: (role) => <Text className="font-medium">{role.name}</Text>,
  },
  {
    key: "members",
    header: "Members",
    width: "100px",
    render: (role) => <Text>{role.memberCount}</Text>,
  },
];

<Table columns={columns} data={roles} rowKey={(row) => row.id} />;
```

For empty and loading states, use the Table's built-in empty surface and the shared `SkeletonTable` from `@/components/ui/Skeleton`. Do not rebuild a one-off empty `<tbody>` or skeleton table.

```tsx
<Table
  columns={columns}
  data={filteredKeys}
  rowKey={(row) => row.id}
  noResultsMessage={<Text>No matching API keys</Text>}
/>
```

Search and filter controls are siblings above the table. On pages, wrap them in `Page.Toolbar` (see the `page-toolbar` skill); the bare `Stack` form below is for non-page surfaces like dialogs and sheets. Keep filter state outside the table, derive filtered rows with `useMemo`, and pass the result to `data`. Use existing controls such as `SearchBar`, `MultiSelect`, `Select`, or page-specific filter pills; do not put form controls inside `Table.Header` unless they are truly column headers. If the table is paginated, pass the filter values to `usePagedRows` `resetOn` (see below) so the page index resets when filters change.

```tsx
const [search, setSearch] = useState("");
const [selectedTags, setSelectedTags] = useState<string[]>([]);

const filteredRows = useMemo(() => {
  const normalizedSearch = search.trim().toLowerCase();

  return rows.filter((row) => {
    const matchesSearch =
      normalizedSearch.length === 0 ||
      row.name.toLowerCase().includes(normalizedSearch);
    const matchesTags =
      selectedTags.length === 0 ||
      row.tags.some((tag) => selectedTags.includes(tag));

    return matchesSearch && matchesTags;
  });
}, [rows, search, selectedTags]);

<Stack direction="horizontal" gap={2} className="mb-4 h-fit">
  <SearchBar
    value={search}
    onChange={setSearch}
    placeholder="Search tools"
    className="w-64"
  />
  <MultiSelect
    options={tagOptions}
    defaultValue={selectedTags}
    onValueChange={setSelectedTags}
    placeholder="Filter by tag"
    autoSize
  />
</Stack>

<Table
  columns={columns}
  data={filteredRows}
  rowKey={(row) => row.id}
  noResultsMessage={<Text>No matching tools</Text>}
/>;
```

Footers that summarize, paginate, or load more rows are sibling bars immediately below the table. For client-side pagination over rows already in memory, use `usePagedRows` and `TablePagination` from `@/components/ui/TablePagination`. Do not hand-roll a "1–10 of 42" footer. `TablePagination` renders nothing when all rows fit on one page. `usePagedRows` resets to the first page when any `resetOn` value changes, so pass the filter and search values there (primitives only, not filter objects) instead of calling `setPage(0)` in each `onChange`.

```tsx
import { TablePagination } from "@/components/ui/TablePagination";
import { usePagedRows } from "@/components/ui/TablePagination/usePagedRows";

const PAGE_SIZE = 25;

const { page, pageRows, setPage } = usePagedRows({
  rows: filteredRows,
  pageSize: PAGE_SIZE,
  resetOn: [search, selectedTags.join(",")],
});

<Table columns={columns} data={pageRows} rowKey={(row) => row.id} />
<TablePagination
  page={page}
  pageSize={PAGE_SIZE}
  totalItems={filteredRows.length}
  onPageChange={setPage}
/>
```

**Never key a data table on filter or search values.** A `key` that changes with the filter remounts the table, and the table flashes its skeleton on every keystroke. Keep the table mounted. For server-filtered data, use `placeholderData: keepPreviousData` on the query so the old rows stay visible while the new ones load. Show the skeleton only on the first load (no data yet). On a filter change, reset only the page index.

Use the compound API only when the body needs custom structure that the declarative API cannot express, such as mixed rows, a full-width CTA row, or a custom no-results branch. Keep the design system wrapper, header, row, and cell components as the default primitives.

```tsx
<Table columns={columns}>
  <Table.Header columns={columns} />
  {items.length === 0 ? (
    <Table.NoResultsMessage>No results found.</Table.NoResultsMessage>
  ) : (
    <Table.Body>
      {items.map((item) => (
        <Table.Row key={item.id} row={item} columns={columns} />
      ))}
    </Table.Body>
  )}
  <Table.Body>
    <Table.Row>
      <td
        colSpan={columns.length}
        className="border-border col-span-full border-t py-5 text-center"
      >
        <Text small muted>
          Want to grant new members access?
        </Text>
        <Button variant="tertiary" size="sm" className="mt-2">
          Configure roles
        </Button>
      </td>
    </Table.Row>
  </Table.Body>
</Table>
```

Use grouped or expandable rows through the table props instead of nesting unrelated cards or custom accordions around a table. Current patterns use `hideHeader` for grouped parent rows and `renderExpandedContent` for nested details.

```tsx
<Table
  columns={groupColumns}
  data={groups}
  rowKey={(row) => row.key}
  hideHeader
  renderExpandedContent={(group) => (
    <Table
      columns={childColumns}
      data={group.items}
      rowKey={(row) => row.id}
      hideHeader
    />
  )}
/>
```

Raw `<tr>` / `<td>` should be rare and stay inside a `<Table.Body>` only when native table semantics are needed and `Table` does not expose them, such as a `colSpan` overflow row. If the row is a normal data row, use `<Table.Row row={row} columns={columns} />` or the declarative `data` prop.
