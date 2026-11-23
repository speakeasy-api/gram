import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Badge } from "@/components/ui/Badge";
import { Card } from "@/components/ui/Card";
import { Skeleton } from "@/components/ui/Skeleton";
import { SkeletonTable } from "@/components/ui/SkeletonTable";
import { Table, type Column } from "@/components/ui/Table";
import { useMemo, useState } from "react";
import { actionDef } from "./action-def";
import { ActionButton } from "./actions";
import { useReportPageTitle } from "./page-title";
import { PanelHeader } from "./PanelHeader";
import type { ResourceDefinition, ResourceModule } from "./types";
import { IdentityCell, MarkTile, ValueView } from "./values";

const NO_ROWS: never[] = [];

/**
 * The list template. Header, toolbar, table, count, and every state (loading,
 * error, empty, no matches) come from the definition; a resource cannot draw
 * any of them differently.
 */
export function ResourceTable<Row extends object, Rec>({
  module,
  onOpen,
}: {
  module: ResourceModule<Row, Rec>;
  onOpen: (id: string) => void;
}): JSX.Element {
  const { definition, data } = module;
  const list = definition.list;
  const query = data.useList();
  const listActions = data.useListActions();
  useReportPageTitle(null);
  const [search, setSearch] = useState("");
  const [filters, setFilters] = useState<Record<string, string>>({});

  const primary = list.primaryAction ? (
    <ActionButton
      def={actionDef(definition, list.primaryAction)}
      handlers={listActions}
      emphasis="primary"
    />
  ) : undefined;

  const rows = query.status === "ready" ? query.data : NO_ROWS;
  const visible = useMemo(() => {
    const needle = search.trim().toLowerCase();
    return rows.filter((row) => {
      if (needle && !list.searchText(row).toLowerCase().includes(needle)) {
        return false;
      }
      return (list.filters ?? []).every((f) => {
        const value = filters[f.id];
        return value == null || value === "all" || f.matches(row, value);
      });
    });
  }, [rows, search, filters, list]);

  const columns = useMemo(() => tableColumns(definition), [definition]);
  const isEmpty = query.status === "ready" && rows.length === 0;

  return (
    <div className="flex flex-col gap-6">
      <PanelHeader
        area={definition.area}
        title={list.title}
        description={list.description}
        action={isEmpty ? undefined : primary}
      />

      {/* The toolbar holds its place while loading so nothing jumps. */}
      {!isEmpty && query.status !== "error" && (
        <div className="flex flex-wrap items-center gap-2">
          <Input
            icon="search"
            aria-label={`Search ${definition.noun.many}`}
            placeholder={`Search ${definition.noun.many}`}
            value={search}
            onChange={setSearch}
            className="min-w-[200px] flex-1"
            disabled={query.status === "loading"}
          />
          {(list.filters ?? []).map((f) => (
            <SegmentedControl
              key={f.id}
              ariaLabel={f.label}
              value={filters[f.id] ?? "all"}
              onChange={(value) =>
                setFilters((prev) => ({ ...prev, [f.id]: value }))
              }
              options={[{ value: "all", label: "All" }, ...f.options]}
              disabled={query.status === "loading"}
            />
          ))}
        </div>
      )}

      {query.status === "loading" && list.layout === "cards" && (
        <div
          role="status"
          aria-label={`Loading ${definition.noun.many}`}
          className="grid grid-cols-[repeat(auto-fill,minmax(280px,1fr))] gap-4"
        >
          {Array.from({ length: 6 }, (_, i) => (
            <Skeleton key={i}>
              <div className="h-[156px] w-full" />
            </Skeleton>
          ))}
        </div>
      )}
      {query.status === "loading" && list.layout !== "cards" && (
        <SkeletonTable
          label={`Loading ${definition.noun.many}`}
          columns={columns.map((c) => c.width)}
        />
      )}

      {query.status === "error" && (
        <InlineEmptyState
          icon="triangle-alert"
          heading={`Couldn't load ${definition.noun.many}`}
          description={query.message}
          action={
            <Button variant="secondary" size="sm" onClick={query.retry}>
              Try again
            </Button>
          }
        />
      )}

      {isEmpty && (
        <InlineEmptyState
          icon={list.empty.icon}
          heading={list.empty.heading}
          description={list.empty.description}
          action={primary}
        />
      )}

      {query.status === "ready" && !isEmpty && (
        <div className="flex flex-col gap-3">
          {list.layout === "cards" ? (
            visible.length === 0 ? (
              <p className="text-muted-foreground border border-dashed px-4 py-8 text-center text-sm">
                No {definition.noun.many} match
                {search ? ` "${search}"` : " these filters"}.
              </p>
            ) : (
              <ul className="grid grid-cols-[repeat(auto-fill,minmax(280px,1fr))] gap-4">
                {visible.map((row) => (
                  <li key={list.rowId(row)}>
                    <ResourceCard
                      definition={definition}
                      row={row}
                      onOpen={() => onOpen(list.rowId(row))}
                    />
                  </li>
                ))}
              </ul>
            )
          ) : (
            <Table
              columns={columns}
              data={visible}
              rowKey={list.rowId}
              onRowClick={(row) => onOpen(list.rowId(row))}
              noResultsMessage={
                <span className="text-muted-foreground">
                  No {definition.noun.many} match
                  {search ? ` "${search}"` : " these filters"}.
                </span>
              }
            />
          )}
          <span className="text-eyebrow">
            {visible.length === rows.length
              ? `${rows.length} ${rows.length === 1 ? definition.noun.one : definition.noun.many}`
              : `${visible.length} of ${rows.length} ${definition.noun.many}`}
          </span>
        </div>
      )}
    </div>
  );
}

function tableColumns<Row extends object, Rec>(
  definition: ResourceDefinition<Row, Rec>,
): Column<Row>[] {
  const cols: Column<Row>[] = definition.list.columns.map((c) =>
    c.kind === "identity"
      ? {
          key: c.id,
          header: c.header,
          width: "3fr",
          render: (row: Row) => <IdentityCell identity={c.value(row)} />,
        }
      : {
          key: c.id,
          header: (
            <span
              className={c.align === "end" ? "block w-full text-right" : ""}
            >
              {c.header}
            </span>
          ),
          width: "1fr",
          render: (row: Row) => (
            <span
              className={
                c.align === "end"
                  ? "flex w-full min-w-0 justify-end"
                  : "flex min-w-0 items-center"
              }
            >
              <ValueView value={c.value(row)} />
            </span>
          ),
        },
  );
  // Every row leads somewhere: the same label on every row.
  cols.push({
    key: "__open",
    header: <span className="sr-only">Open</span>,
    width: "96px",
    render: () => (
      <span className="text-muted-foreground block w-full text-right text-sm whitespace-nowrap">
        Open →
      </span>
    ),
  });
  return cols;
}

/** One row as a card: the identity on the rail, the story beside it. */
function ResourceCard<Row extends object, Rec>({
  definition,
  row,
  onOpen,
}: {
  definition: ResourceDefinition<Row, Rec>;
  row: Row;
  onOpen: () => void;
}): JSX.Element | null {
  const identityColumn = definition.list.columns.find(
    (c) => c.kind === "identity",
  );
  if (!identityColumn || identityColumn.kind !== "identity") return null;
  const identity = identityColumn.value(row);
  const badge = definition.list.card?.badge?.(row);
  const facts = definition.list.card?.facts?.(row) ?? [];
  return (
    <Card.Entity
      onClick={onOpen}
      iconRailClassName="w-24"
      icon={<MarkTile mark={identity.mark} size={40} />}
      overlay={
        badge && (
          <div className="absolute top-3 left-3 z-10">
            <Badge variant={badge.tone}>
              <Badge.Text>{badge.text}</Badge.Text>
            </Badge>
          </div>
        )
      }
    >
      <span className="truncate" title={identity.title}>
        {identity.title}
      </span>
      {identity.subtitle && (
        <span className="text-muted-foreground mt-1 line-clamp-3 text-sm leading-snug">
          {identity.subtitle}
        </span>
      )}
      {facts.length > 0 && (
        <span className="mt-auto flex flex-wrap items-center gap-x-4 gap-y-1 pt-3 text-sm">
          {facts.map((fact, i) => (
            <ValueView key={i} value={fact} />
          ))}
        </span>
      )}
    </Card.Entity>
  );
}
