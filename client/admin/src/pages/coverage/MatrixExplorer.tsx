import {
  accountTypes,
  accountLabels,
  type AccountFilter,
  type AccountType,
} from "./accounts";
import { useCatalog } from "./catalogContext";
import { useState, type JSX } from "react";
import {
  ArrowLeftRight,
  Download,
  UserRound,
  UsersRound,
  Building2,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@/components/ui/tooltip";
import { Input } from "@/components/ui/input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { matrixCsv, downloadMatrixCsv } from "./csv";
import { resolveMatrixCell } from "./matrixCell";
import { CoverageTooltip } from "./CoverageTooltip";
import { IntegrationRequirements } from "./IntegrationRequirements";
import { Choice } from "./CoverageEditor";
import {
  mappingKey,
  statusLabels,
  symbols,
  type Capability,
  type Draft,
  type Catalog,
  type Fact,
  type Mapping,
  type Method,
  type Product,
} from "./model";

import "./platform-headers.css";
import "./support-status.css";

type Axis = "methods" | "platforms" | "capabilities";
type Item = { id: string; name: string; group: string; platform?: Product };
export type Selection = {
  account?: AccountFilter;
  capability: Capability;
  method?: Method;
  product?: Product;
};
const axisOptions = [
  { value: "methods", label: "Integration methods" },
  { value: "platforms", label: "Platforms" },
  { value: "capabilities", label: "Capabilities" },
];
function catalogItems({
  methods,
  products,
  capabilities,
}: Catalog): Record<Axis, Item[]> {
  return {
    methods: methods.map((method) => ({ ...method, group: method.vendor })),
    platforms: products.map((product) => ({
      ...product,
      group: product.family,
      platform: product,
    })),
    capabilities,
  };
}
const applicabilityOptions = [
  { value: "unknown", label: "? Unknown" },
  { value: "applicable", label: "✓ Applies" },
  { value: "na", label: "— Does not apply" },
];

function headerIdentity(axis: Axis, item: Item) {
  const platform = axis === "platforms" ? item.platform : undefined;
  if (!platform)
    return {
      title: item.name,
      detail: item.group,
      vendor: undefined,
      family: undefined,
    };
  const title =
    platform.family === "Other Claudes" ? platform.surface : platform.family;
  const detail =
    platform.surface === "App" || title.endsWith(platform.surface)
      ? undefined
      : platform.surface;
  return { title, detail, vendor: platform.vendor, family: platform.family };
}

function HeaderLabel({
  axis,
  item,
  highlighted,
}: {
  axis: Axis;
  item: Item;
  highlighted: boolean;
}): JSX.Element {
  const identity = headerIdentity(axis, item);
  return (
    <>
      <span className="font-semibold">
        {highlighted && <span aria-hidden="true">✓ </span>}
        {identity.title}
      </span>
      {identity.detail && (
        <span className="mt-0.5 block text-[11px] font-normal">
          {identity.detail}
        </span>
      )}
    </>
  );
}

function AxisPicker({
  label,
  axis,
  selected,
  onAxis,
  onSelect,
}: {
  label: string;
  axis: Axis;
  selected: string[];
  onAxis: (axis: Axis) => void;
  onSelect: (ids: string[]) => void;
}): JSX.Element {
  const catalog = useCatalog();
  const { products } = catalog;
  const items = catalogItems(catalog);
  const [search, setSearch] = useState("");
  const options =
    axis === "platforms"
      ? [...new Set(products.map((product) => product.family))].map(
          (family) => ({
            id: family,
            name: family,
            group: products.find((product) => product.family === family)!
              .vendor,
            ids: products
              .filter((product) => product.family === family)
              .map((product) => product.id),
          }),
        )
      : items[axis].map((item) => ({ ...item, ids: [item.id] }));
  const selectedCount = options.filter((item) =>
    item.ids.some((id) => selected.includes(id)),
  ).length;
  const matches = options.filter((item) =>
    `${item.group} ${item.name}`.toLowerCase().includes(search.toLowerCase()),
  );
  return (
    <div className="flex flex-col gap-1">
      <span className="text-muted-foreground text-xs font-medium">{label}</span>
      <div className="flex gap-2">
        <div className="w-44">
          <Choice
            label={label}
            value={axis}
            options={axisOptions}
            onChange={(value) => onAxis(value as Axis)}
          />
        </div>
        <Popover>
          <PopoverTrigger asChild>
            <Button
              variant="outline"
              aria-label={`Choose ${label.toLowerCase()}`}
            >
              {selectedCount} {axis === "platforms" ? "families" : "selected"}
            </Button>
          </PopoverTrigger>
          <PopoverContent align="start" className="w-80 space-y-3">
            <Input
              aria-label={`Search ${label.toLowerCase()}`}
              placeholder="Search…"
              value={search}
              onChange={(event) => setSearch(event.target.value)}
            />
            <div className="flex gap-2">
              <Button
                variant="ghost"
                size="sm"
                onClick={() => onSelect(items[axis].map((item) => item.id))}
              >
                Select all
              </Button>
              <Button variant="ghost" size="sm" onClick={() => onSelect([])}>
                Clear
              </Button>
            </div>
            <div className="max-h-72 space-y-1 overflow-auto">
              {matches.map((item) => (
                <label
                  key={item.id}
                  className="hover:bg-accent flex cursor-pointer items-center gap-3 rounded-md p-2 text-sm"
                >
                  <input
                    type="checkbox"
                    aria-label={item.name}
                    checked={item.ids.every((id) => selected.includes(id))}
                    ref={(node) => {
                      if (node)
                        node.indeterminate =
                          item.ids.some((id) => selected.includes(id)) &&
                          !item.ids.every((id) => selected.includes(id));
                    }}
                    onChange={(event) =>
                      onSelect(
                        event.target.checked
                          ? [...new Set([...selected, ...item.ids])]
                          : selected.filter((id) => !item.ids.includes(id)),
                      )
                    }
                  />
                  <span>
                    {item.name}
                    <span className="text-muted-foreground block text-xs">
                      {item.group}
                    </span>
                  </span>
                </label>
              ))}
              {!matches.length && (
                <p className="text-muted-foreground p-2 text-sm">No matches.</p>
              )}
            </div>
          </PopoverContent>
        </Popover>
      </div>
    </div>
  );
}

const accountIcons = {
  personal: UserRound,
  team: UsersRound,
  enterprise: Building2,
};

function AccountIcons({
  facts,
}: {
  facts: Record<AccountType, Fact>;
}): JSX.Element {
  return (
    <span className="flex items-center justify-center gap-3">
      {accountTypes.map((account) => {
        const Icon = accountIcons[account];
        const fact = facts[account];
        const label = `${accountLabels[account]}: ${statusLabels[fact.status]}${fact.verify ? ", needs verification" : ""}`;
        return (
          <span
            key={account}
            aria-label={label}
            className={
              fact.status === "supported"
                ? "text-foreground"
                : "text-muted-foreground/35"
            }
          >
            <Icon className="size-4" aria-hidden="true" />
          </span>
        );
      })}
    </span>
  );
}

function FactCell({
  cell,
  accounts,
  account,
  label,
  onClick,
}: {
  cell: ReturnType<typeof resolveMatrixCell>;
  accounts?: Record<AccountType, ReturnType<typeof resolveMatrixCell>>;
  account: AccountFilter;
  label: string;
  onClick: () => void;
}): JSX.Element {
  const { fact } = cell;
  return (
    <Tooltip delayDuration={450}>
      <TooltipTrigger asChild>
        <button
          type="button"
          onClick={onClick}
          aria-label={`${label}: ${accounts ? accountTypes.map((account) => `${accountLabels[account]}: ${statusLabels[accounts[account].fact.status]}`).join(", ") : statusLabels[fact.status]}${fact.verify ? ", needs verification" : ""}`}
          data-support-status={accounts ? "accounts" : fact.status}
          className="support-status focus-visible:ring-ring flex min-h-10 w-full items-center justify-center gap-1.5 px-1.5 py-1 text-xs focus-visible:ring-2 focus-visible:ring-inset"
        >
          {accounts ? (
            <AccountIcons
              facts={{
                personal: accounts.personal.fact,
                team: accounts.team.fact,
                enterprise: accounts.enterprise.fact,
              }}
            />
          ) : (
            <>
              <span className="shrink-0 text-base">
                {symbols[fact.status]}
                {fact.verify && <sup>*</sup>}
              </span>
              <span className="min-w-0 truncate">
                {statusLabels[fact.status]}
              </span>
            </>
          )}
        </button>
      </TooltipTrigger>
      <TooltipContent
        side="top"
        sideOffset={6}
        className="max-w-sm space-y-3 text-left"
      >
        <p className="font-semibold">{label}</p>
        {accounts ? (
          accountTypes.map((type) => (
            <CoverageTooltip key={type} account={type} cell={accounts[type]} />
          ))
        ) : (
          <CoverageTooltip account={account} cell={cell} />
        )}
        <p className="text-xs opacity-75">Click to inspect or edit coverage.</p>
      </TooltipContent>
    </Tooltip>
  );
}

export function MatrixExplorer({
  draft,
  onSave,
  onSelect,
}: {
  draft: Draft;
  onSave: (draft: Draft) => Promise<boolean>;
  onSelect: (selection: Selection) => void;
}): JSX.Element {
  const catalog = useCatalog();
  const { methods } = catalog;
  const [account, setAccount] = useState<AccountFilter>("all");
  const items = catalogItems(catalog);
  const [axes, setAxes] = useState<{ rows: Axis; columns: Axis }>({
    rows: "platforms",
    columns: "capabilities",
  });
  const [highlighted, setHighlighted] = useState<Record<Axis, string[]>>({
    methods: [],
    platforms: [],
    capabilities: [],
  });
  const [selected, setSelected] = useState<Record<Axis, string[]>>({
    methods: items.methods.map((item) => item.id),
    platforms: items.platforms.map((item) => item.id),
    capabilities: items.capabilities.map((item) => item.id),
  });
  const [slices, setSlices] = useState<Record<Axis, string>>({
    methods: "all",
    platforms: "all",
    capabilities: "all",
  });
  const third = axisOptions.find(
    (option) => option.value !== axes.rows && option.value !== axes.columns,
  )!.value as Axis;
  const rows = items[axes.rows].filter((item) =>
    selected[axes.rows].includes(item.id),
  );
  const columns = items[axes.columns]
    .filter((item) => selected[axes.columns].includes(item.id))
    .sort((a, b) => {
      if (axes.columns !== "capabilities") return 0;
      return (
        Number(a.id.endsWith("-redaction")) -
        Number(b.id.endsWith("-redaction"))
      );
    });
  const rowHighlights = highlighted[axes.rows].filter((id) =>
    rows.some((row) => row.id === id),
  );
  const columnHighlights = highlighted[axes.columns].filter((id) =>
    columns.some((column) => column.id === id),
  );
  function isFocused(axis: Axis, id: string) {
    const active = axis === axes.rows ? rowHighlights : columnHighlights;
    return !active.length || active.includes(id);
  }
  function toggleHighlight(axis: Axis, id: string) {
    setHighlighted((current) => ({
      ...current,
      [axis]: current[axis].includes(id)
        ? current[axis].filter((item) => item !== id)
        : [...current[axis], id],
    }));
  }
  const scope: Record<Axis, string[]> = {
    methods: [],
    platforms: [],
    capabilities: [],
  };
  scope[axes.rows] = rows
    .filter((row) => isFocused(axes.rows, row.id))
    .map((row) => row.id);
  scope[axes.columns] = columns
    .filter((column) => isFocused(axes.columns, column.id))
    .map((column) => column.id);
  const sliceId = slices[third];
  scope[third] = sliceId === "all" ? [] : [sliceId];
  if (third === "methods" && sliceId === "all")
    scope.methods = methods.map((method) => method.id);

  const sliceLabels: Record<Axis, string> = {
    methods: "Across all methods",
    platforms: "Method reference claims",
    capabilities: "Applicability only",
  };
  function changeAxis(position: "rows" | "columns", axis: Axis) {
    const other = position === "rows" ? "columns" : "rows";
    setAxes({
      ...axes,
      [position]: axis,
      [other]: axes[other] === axis ? axes[position] : axes[other],
    });
  }
  const exportRows = rows.filter((row) => isFocused(axes.rows, row.id));
  const exportColumns = columns.filter((column) =>
    isFocused(axes.columns, column.id),
  );
  function exportCsv() {
    const context =
      sliceId === "all"
        ? sliceLabels[third]
        : items[third].find((item) => item.id === sliceId)!.name;
    const heading = `${axisOptions.find((option) => option.value === axes.rows)!.label} (${context}; ${account === "all" ? "All account types" : accountLabels[account]})`;
    const contents = [
      [heading, ...exportColumns.map((column) => column.name)],
      ...exportRows.map((row) => [
        row.name,
        ...exportColumns.map((column) => {
          const cell = resolveMatrixCell(
            draft,
            catalog,
            {
              [axes.rows]: row.id,
              [axes.columns]: column.id,
              [third]: sliceId,
            },
            account,
          );
          if (!cell.capability) {
            const labels = {
              unknown: "Unknown",
              applicable: "Applies",
              na: "Does not apply",
            };
            return [labels[cell.mapping.applicability], cell.mapping.conditions]
              .filter(Boolean)
              .join("; ");
          }
          return [
            account === "all"
              ? accountTypes
                  .map(
                    (type) =>
                      `${accountLabels[type]}: ${statusLabels[resolveMatrixCell(draft, catalog, { [axes.rows]: row.id, [axes.columns]: column.id, [third]: sliceId }, type).fact.status]}`,
                  )
                  .join("; ")
              : statusLabels[cell.fact.status],
            cell.fact.note,
            cell.fact.verify ? "Needs verification" : "",
            cell.mapping.conditions,
          ]
            .filter(Boolean)
            .join("; ");
        }),
      ]),
    ];
    downloadMatrixCsv(
      matrixCsv(contents),
      `support-matrix-${axes.rows}-by-${axes.columns}.csv`,
    );
  }
  function renderCell(row: Item, column: Item): JSX.Element {
    const ids = {
      [axes.rows]: row.id,
      [axes.columns]: column.id,
      [third]: sliceId,
    };
    const cell = resolveMatrixCell(draft, catalog, ids, account);
    const { method, product, capability, mapping } = cell;
    const label = `${row.name} × ${column.name}`;
    if (!capability && method && product) {
      const key = mappingKey(method.id, product.id);
      return (
        <Choice
          label={label}
          value={mapping.applicability}
          options={applicabilityOptions}
          onChange={(value) =>
            void onSave({
              ...draft,
              mappings: {
                ...draft.mappings,
                [key]: {
                  ...mapping,
                  applicability: value as Mapping["applicability"],
                },
              },
            })
          }
        />
      );
    }
    if (!capability) return <span>Unknown</span>;
    return (
      <FactCell
        cell={cell}
        account={account}
        accounts={
          account === "all"
            ? (Object.fromEntries(
                accountTypes.map((type) => [
                  type,
                  resolveMatrixCell(draft, catalog, ids, type),
                ]),
              ) as Record<AccountType, ReturnType<typeof resolveMatrixCell>>)
            : undefined
        }
        label={label}
        onClick={() => onSelect({ method, product, capability, account })}
      />
    );
  }
  return (
    <>
      <div className="flex flex-wrap items-end gap-3 border-y py-2">
        <AxisPicker
          label="Rows"
          axis={axes.rows}
          selected={selected[axes.rows]}
          onAxis={(axis) => changeAxis("rows", axis)}
          onSelect={(ids) => setSelected({ ...selected, [axes.rows]: ids })}
        />
        <Button
          variant="ghost"
          size="icon"
          aria-label="Swap rows and columns"
          onClick={() => setAxes({ rows: axes.columns, columns: axes.rows })}
        >
          <ArrowLeftRight className="size-4" />
        </Button>
        <AxisPicker
          label="Columns"
          axis={axes.columns}
          selected={selected[axes.columns]}
          onAxis={(axis) => changeAxis("columns", axis)}
          onSelect={(ids) => setSelected({ ...selected, [axes.columns]: ids })}
        />
        <div className="flex min-w-52 flex-col gap-1">
          <span className="text-muted-foreground text-xs font-medium">
            {axisOptions.find((option) => option.value === third)!.label}
          </span>
          <Choice
            label="Remaining dimension"
            value={sliceId}
            options={[
              { value: "all", label: sliceLabels[third] },
              ...items[third].map((item) => ({
                value: item.id,
                label: item.name,
              })),
            ]}
            onChange={(value) => setSlices({ ...slices, [third]: value })}
          />
        </div>
        <div className="flex min-w-44 flex-col gap-1">
          <span className="text-muted-foreground text-xs font-medium">
            Account type
          </span>
          <Choice
            label="Account type"
            value={account}
            options={[
              { value: "all", label: "All account types" },
              ...accountTypes.map((type) => ({
                value: type,
                label: accountLabels[type],
              })),
            ]}
            onChange={(value) => setAccount(value as AccountFilter)}
          />
        </div>
      </div>
      <IntegrationRequirements
        draft={draft}
        account={account}
        platformIds={scope.platforms}
        capabilityIds={scope.capabilities}
        methodIds={scope.methods}
        scoped={third === "methods" || sliceId !== "all"}
      />
      <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
        <p className="text-muted-foreground">
          {third === "capabilities" && sliceId === "all"
            ? "Set whether each method applies to each platform. Changes save immediately."
            : "Click a cell to inspect coverage, conditions, and contributing methods."}
        </p>
        <div className="flex items-center gap-3">
          <span className="text-muted-foreground text-xs">
            {rows.length} rows × {columns.length} columns
          </span>
          <Button
            variant="outline"
            size="sm"
            onClick={exportCsv}
            disabled={!exportRows.length || !exportColumns.length}
            title={`Export ${exportRows.length} rows × ${exportColumns.length} columns; highlights narrow the export`}
          >
            <Download className="size-4" />
            Export CSV
          </Button>
        </div>
      </div>
      <div className="flex flex-wrap items-center justify-between gap-2 text-xs">
        <p className="text-muted-foreground">
          Click row or column headers to focus multiple items. Click again to
          deselect; click cells to edit.
        </p>
        {(rowHighlights.length > 0 || columnHighlights.length > 0) && (
          <Button
            variant="outline"
            size="sm"
            onClick={() =>
              setHighlighted({ methods: [], platforms: [], capabilities: [] })
            }
          >
            Clear highlights
          </Button>
        )}
      </div>
      <div
        aria-label="Support level legend"
        className="flex flex-wrap items-center gap-1.5 text-xs"
      >
        {account === "all" && (
          <span className="text-muted-foreground flex items-center gap-3">
            {accountTypes.map((type) => {
              const Icon = accountIcons[type];
              return (
                <span key={type} className="inline-flex items-center gap-1">
                  <Icon className="size-4" />
                  {accountLabels[type]}
                </span>
              );
            })}{" "}
            · Dark: supported · Grey: no confirmed full support
          </span>
        )}
        {account !== "all" &&
          Object.entries(statusLabels).map(([status, label]) => (
            <span
              key={status}
              data-support-status={status}
              className="support-status inline-flex items-center gap-1 rounded-sm px-2 py-1"
            >
              <span aria-hidden="true">
                {symbols[status as Fact["status"]]}
              </span>
              {label}
            </span>
          ))}
        <span className="text-muted-foreground">* Needs verification</span>
      </div>
      <div className="min-h-0 rounded-lg border [&>[data-slot=table-container]]:max-h-[65vh] [&>[data-slot=table-container]]:overflow-auto">
        <Table
          className="table-fixed text-xs"
          style={{ minWidth: 176 + columns.length * 120 }}
        >
          <colgroup>
            <col style={{ width: 176 }} />
            {columns.map((column) => (
              <col key={column.id} />
            ))}
          </colgroup>
          <TableHeader>
            <TableRow>
              <TableHead className="bg-primary text-primary-foreground sticky top-0 left-0 z-30 border-r px-2">
                {
                  axisOptions.find((option) => option.value === axes.rows)!
                    .label
                }
              </TableHead>
              {columns.map((column) => (
                <TableHead
                  key={column.id}
                  data-vendor={headerIdentity(axes.columns, column).vendor}
                  data-family={headerIdentity(axes.columns, column).family}
                  className={`sticky top-0 z-20 border-r p-0 text-center whitespace-normal ${axes.columns === "platforms" ? "support-platform-header" : "bg-primary text-primary-foreground"}`}
                >
                  <button
                    type="button"
                    aria-label={`Highlight column ${column.name}`}
                    aria-pressed={columnHighlights.includes(column.id)}
                    onClick={() => toggleHighlight(axes.columns, column.id)}
                    className={`focus-visible:ring-ring w-full px-2 py-2 focus-visible:ring-2 ${isFocused(axes.columns, column.id) ? "opacity-100" : "opacity-35"}`}
                  >
                    <HeaderLabel
                      axis={axes.columns}
                      item={column}
                      highlighted={columnHighlights.includes(column.id)}
                    />
                  </button>
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.length > 0 && columns.length > 0 ? (
              rows.map((row, index) => (
                <TableRow
                  key={row.id}
                  className={
                    index % 2 === 0
                      ? "bg-card hover:bg-card"
                      : "bg-muted hover:bg-muted"
                  }
                >
                  <TableCell
                    data-vendor={headerIdentity(axes.rows, row).vendor}
                    data-family={headerIdentity(axes.rows, row).family}
                    className={`sticky left-0 z-10 border-r p-0 whitespace-normal ${axes.rows === "platforms" ? "support-platform-header" : "bg-inherit"}`}
                  >
                    <button
                      type="button"
                      aria-label={`Highlight row ${row.name}`}
                      aria-pressed={rowHighlights.includes(row.id)}
                      onClick={() => toggleHighlight(axes.rows, row.id)}
                      className={`focus-visible:ring-ring w-full px-2 py-1.5 text-left focus-visible:ring-2 ${isFocused(axes.rows, row.id) ? "opacity-100" : "opacity-35"}`}
                    >
                      <HeaderLabel
                        axis={axes.rows}
                        item={row}
                        highlighted={rowHighlights.includes(row.id)}
                      />
                    </button>
                  </TableCell>
                  {columns.map((column) => (
                    <TableCell key={column.id} className="border-r p-0">
                      <div
                        data-dimmed={
                          !isFocused(axes.rows, row.id) ||
                          !isFocused(axes.columns, column.id)
                        }
                        className={
                          isFocused(axes.rows, row.id) &&
                          isFocused(axes.columns, column.id)
                            ? "opacity-100"
                            : "opacity-30"
                        }
                      >
                        {renderCell(row, column)}
                      </div>
                    </TableCell>
                  ))}
                </TableRow>
              ))
            ) : (
              <TableRow>
                <TableCell
                  colSpan={columns.length + 1}
                  className="text-muted-foreground p-10 text-center"
                >
                  Choose at least one row and one column to build your matrix.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>
      <p className="text-muted-foreground text-xs">
        Choose which items appear using “selected” beside each axis. Changing
        the view never changes your saved coverage.
      </p>
    </>
  );
}
