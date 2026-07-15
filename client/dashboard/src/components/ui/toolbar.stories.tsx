import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";
import { Toolbar } from "@/components/ui/toolbar";
import type { ViewMode } from "@/components/ui/use-view-mode";

/**
 * `Toolbar` (rendered on pages as `Page.Toolbar`) is a compound control bar:
 * Search + Filters stay left, Sort/Count/ViewAs/Actions/Refresh anchor right.
 *
 * `Toolbar.Filters` is omitted from these stories — it hard-requires a
 * `FilterDimension[]` schema plus a page-supplied `optionsById` map that in the
 * app comes from per-page data hooks (server lists, policy lists, etc.). The
 * schema type itself has no realistic story-local stand-in without dragging in
 * a page's real filter config, so these stories show the rest of the toolbar
 * (Search, SortBy, Count, ViewAs, Refresh, Actions) composed the way a real
 * list page does.
 */
const meta: Meta<typeof Toolbar> = {
  title: "UI/Toolbar",
  component: Toolbar,
  parameters: {
    layout: "padded",
  },
};

export default meta;

type Story = StoryObj<typeof Toolbar>;

const SORT_OPTIONS = [
  { value: "popular", label: "Most Popular" },
  { value: "recent", label: "Recently Added" },
  { value: "alphabetical", label: "A → Z" },
];

function ListPageToolbar() {
  const [search, setSearch] = useState("");
  const [sort, setSort] = useState("popular");
  const [view, setView] = useState<ViewMode>("grid");

  return (
    <Toolbar>
      <Toolbar.Search
        value={search}
        onChange={setSearch}
        placeholder="Search MCP servers..."
      />
      <Toolbar.SortBy value={sort} onChange={setSort} options={SORT_OPTIONS} />
      <Toolbar.Count>24 servers</Toolbar.Count>
      <Toolbar.ViewAs value={view} onChange={setView} />
    </Toolbar>
  );
}

export const ListPage: Story = {
  render: () => <ListPageToolbar />,
};

function ToolbarWithSortDirection() {
  const [search, setSearch] = useState("");
  const [sort, setSort] = useState("recent");
  const [direction, setDirection] = useState<"asc" | "desc">("desc");

  return (
    <Toolbar>
      <Toolbar.Search value={search} onChange={setSearch} />
      <Toolbar.SortBy
        value={sort}
        onChange={setSort}
        options={SORT_OPTIONS}
        direction={direction}
        onDirectionChange={setDirection}
      />
    </Toolbar>
  );
}

export const WithSortDirection: Story = {
  render: () => <ToolbarWithSortDirection />,
};

function ToolbarWithRefresh() {
  const [search, setSearch] = useState("");
  const [isRefreshing, setIsRefreshing] = useState(false);

  return (
    <Toolbar>
      <Toolbar.Search value={search} onChange={setSearch} />
      <Toolbar.Count>128 results</Toolbar.Count>
      <Toolbar.Refresh
        isRefreshing={isRefreshing}
        onRefresh={() => {
          setIsRefreshing(true);
          setTimeout(() => setIsRefreshing(false), 1200);
        }}
      />
    </Toolbar>
  );
}

export const WithRefresh: Story = {
  render: () => <ToolbarWithRefresh />,
};

function ToolbarWithActions() {
  const [search, setSearch] = useState("");
  const [view, setView] = useState<ViewMode>("table");

  return (
    <Toolbar>
      <Toolbar.Search
        value={search}
        onChange={setSearch}
        placeholder="Search chats..."
      />
      <Toolbar.Actions>
        <div className="border-border bg-card flex h-10 items-center rounded-md border px-3 text-sm">
          Tokens
        </div>
      </Toolbar.Actions>
      <Toolbar.ViewAs value={view} onChange={setView} />
    </Toolbar>
  );
}

export const WithCustomActions: Story = {
  render: () => <ToolbarWithActions />,
};

function SearchOnlyToolbar() {
  const [search, setSearch] = useState("");
  return (
    <Toolbar>
      <Toolbar.Search
        value={search}
        onChange={setSearch}
        placeholder="Search..."
      />
    </Toolbar>
  );
}

export const SearchOnly: Story = {
  render: () => <SearchOnlyToolbar />,
};
