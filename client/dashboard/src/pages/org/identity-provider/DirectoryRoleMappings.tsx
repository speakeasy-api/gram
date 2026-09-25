import { useQueryClient } from "@tanstack/react-query";
import {
  Check,
  ChevronRight,
  Loader2,
  Plus,
  RefreshCw,
  Trash2,
  TriangleAlert,
} from "lucide-react";
import {
  cloneElement,
  type ReactElement,
  type ReactNode,
  useDeferredValue,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { useNavigate, useSearchParams } from "react-router";
import { toast } from "sonner";

import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { Combobox, type DropdownItem } from "@/components/ui/Combobox";
import { SearchBar } from "@/components/ui/SearchBar";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { type Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { cn } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import type { DirectoryRoleMapping } from "@gram/client/models/components/directoryrolemapping.js";
import type { ListDirectoryRoleMappingsResult } from "@gram/client/models/components/listdirectoryrolemappingsresult.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { SetDirectoryRoleMappingForm } from "@gram/client/models/components/setdirectoryrolemappingform.js";
import { useDeleteDirectoryRoleMappingMutation } from "@gram/client/react-query/deleteDirectoryRoleMapping.js";
import {
  invalidateAllDirectoryRoleMappings,
  useDirectoryRoleMappings,
} from "@gram/client/react-query/directoryRoleMappings.js";
import {
  invalidateAllRoles,
  useRoles,
} from "@gram/client/react-query/roles.js";
import { useSetDirectoryRoleMappingMutation } from "@gram/client/react-query/setDirectoryRoleMapping.js";
import { useSyncDirectoryGroupsMutation } from "@gram/client/react-query/syncDirectoryGroups.js";

import {
  clearPendingMappingParams,
  createRoleForMappingParams,
  pendingMappingFromParams,
} from "./directoryMappingFlow";

const CREATE_ROLE = "__create_role";

/** One directory group or attribute value that can be mapped to a role. */
type SourceRow = {
  key: string;
  label: string;
  detail: string;
  form: Omit<SetDirectoryRoleMappingForm, "roleUrn">;
  mapping: DirectoryRoleMapping | undefined;
};

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback;
}

function memberLabel(count: number): string {
  return `${count} ${count === 1 ? "user" : "users"}`;
}

function groupRows(data: ListDirectoryRoleMappingsResult): SourceRow[] {
  const byGroup = new Map(
    data.mappings
      .filter((m) => m.sourceKind === "group")
      .map((m) => [m.directoryGroupId, m]),
  );
  return data.groups.map((group) => ({
    key: group.id,
    label: group.name,
    detail: memberLabel(group.memberCount),
    form: { sourceKind: "group", directoryGroupId: group.id },
    mapping: byGroup.get(group.id),
  }));
}

/** Attribute mappings that exist, including ones whose value no user has now. */
function attributeMappingRows(
  data: ListDirectoryRoleMappingsResult,
): SourceRow[] {
  return data.mappings
    .filter((m) => m.sourceKind === "attribute")
    .map((mapping): SourceRow => {
      const key = mapping.attributeKey ?? "";
      const value = mapping.attributeValue ?? "";
      const option = data.attributes.find(
        (a) => a.key === key && a.value === value,
      );
      return {
        key: mapping.id,
        label: `${key} = ${value}`,
        detail: memberLabel(option?.memberCount ?? 0),
        form: {
          sourceKind: "attribute",
          attributeKey: key,
          attributeValue: value,
        },
        mapping,
      };
    })
    .sort((a, b) => a.label.localeCompare(b.label));
}

/**
 * Maps directory groups to Gram roles, with attribute values as a secondary
 * option for directories whose groups don't fit. Every group is a row with its
 * own role picker; picking a role saves it. Members who match get that role on
 * top of the roles assigned to them directly. Render it only for org admins:
 * the listing exposes directory attribute values and the server rejects anyone
 * else.
 */
export function DirectoryRoleMappings({
  footerAction,
}: {
  /** Shown at the right of the footer bar, such as the connection button. */
  footerAction?: ReactNode;
}): JSX.Element {
  const { data, isPending } = useDirectoryRoleMappings();
  const { data: rolesData } = useRoles();
  const [search, setSearch] = useState("");
  const deferredSearch = useDeferredValue(search);

  // The API orders roles by slug; the picker lists them by name, ignoring case.
  const roles = useMemo(
    () =>
      [...(rolesData?.roles ?? [])].sort((a, b) =>
        a.name.localeCompare(b.name, undefined, { sensitivity: "base" }),
      ),
    [rolesData?.roles],
  );
  const rows = useMemo(() => (data ? groupRows(data) : []), [data]);

  // Back from creating a role for a group or attribute: map it, then drop the
  // round-trip parameters so a reload does not save it again.
  const [params, setParams] = useSearchParams();
  const pending = pendingMappingFromParams(params);
  const queryClient = useQueryClient();
  const savePending = useSetDirectoryRoleMappingMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllDirectoryRoleMappings(queryClient),
        invalidateAllRoles(queryClient),
      ]);
      toast.success("Role created and mapped");
    },
    onError: (error) => {
      toast.error(errorMessage(error, "Failed to map the new role"));
    },
  });
  const savedPending = useRef<string | null>(null);
  useEffect(() => {
    if (!pending) return;
    const pendingKey = JSON.stringify(pending);
    if (savedPending.current === pendingKey) return;
    savedPending.current = pendingKey;
    setParams((previous) => clearPendingMappingParams(previous), {
      replace: true,
    });
    savePending.mutate({ request: { setDirectoryRoleMappingForm: pending } });
  }, [pending, savePending, setParams]);

  // Unmapped groups sort first. Each row's place is fixed by whether it was
  // mapped when it first loaded, so picking a role does not move the row
  // under the cursor; the order refreshes the next time the panel mounts.
  const [mappedAtLoad, setMappedAtLoad] = useState<Record<string, boolean>>({});
  useEffect(() => {
    setMappedAtLoad((previous) => {
      const missing = rows.filter((row) => !(row.key in previous));
      if (missing.length === 0) return previous;
      const next = { ...previous };
      for (const row of missing) next[row.key] = row.mapping !== undefined;
      return next;
    });
  }, [rows]);

  const visibleRows = useMemo(() => {
    const needle = deferredSearch.trim().toLowerCase();
    const wasMapped = (row: SourceRow) =>
      mappedAtLoad[row.key] ?? row.mapping !== undefined;
    return rows
      .filter(
        (row) => needle === "" || row.label.toLowerCase().includes(needle),
      )
      .sort(
        (a, b) =>
          Number(wasMapped(a)) - Number(wasMapped(b)) ||
          a.label.localeCompare(b.label),
      );
  }, [rows, deferredSearch, mappedAtLoad]);

  const mappedCount = rows.filter((row) => row.mapping).length;
  const attributeCount =
    data?.mappings.filter((m) => m.sourceKind === "attribute").length ?? 0;

  return (
    <div className="mt-4 space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <div className="text-eyebrow">Configure role mappings</div>
          <Text muted small>
            {mappedCount} of {rows.length} groups give their members a role.
          </Text>
        </div>
        <div className="flex items-center gap-2">
          {rows.length > 0 && (
            <SearchBar
              value={search}
              onChange={setSearch}
              placeholder="Search groups"
              className="w-56"
            />
          )}
          <SyncGroupsButton />
        </div>
      </div>

      <Collapsible>
        {isPending && <SkeletonTable />}
        {!isPending && rows.length === 0 && (
          <InlineEmptyState
            icon="users"
            heading="No directory groups yet"
            description="Sync groups to pull them from your directory."
          />
        )}
        {!isPending && rows.length > 0 && (
          <MappingTable
            rows={visibleRows}
            roles={roles}
            sourceHeader="Directory group"
            noResults="No groups match"
            scrollable
          />
        )}
        {/* One footer bar under the table: the attribute fallback on the left,
          the directory connection on the right. */}
        <div
          className={cn(
            "border-border flex flex-wrap items-center justify-between gap-3 border px-2 py-2",
            rows.length > 0 && "border-t-0",
          )}
        >
          <CollapsibleTrigger asChild>
            <Button variant="tertiary" size="sm" className="group">
              <Button.LeftIcon>
                <ChevronRight className="size-4 transition-transform group-data-[state=open]:rotate-90" />
              </Button.LeftIcon>
              Map by attribute
              {attributeCount > 0 && ` (${attributeCount})`}
            </Button>
          </CollapsibleTrigger>
          {footerAction}
        </div>
        <CollapsibleContent className="border-border space-y-2 border border-t-0 p-4">
          {data && <AttributeMappings data={data} roles={roles} />}
        </CollapsibleContent>
      </Collapsible>
    </div>
  );
}

/**
 * One table of groups or attribute values, each with its role picker and a
 * mark saying whether it gives a role. Groups and attributes share it so the
 * two sections look and behave the same.
 */
function MappingTable({
  rows,
  roles,
  sourceHeader,
  noResults,
  scrollable = false,
  removable = false,
}: {
  rows: SourceRow[];
  roles: Role[];
  sourceHeader: string;
  noResults: string;
  scrollable?: boolean;
  /**
   * Shows a remove button in place of the mark, for tables where every row is
   * a mapping (attribute values), so the mark would say nothing.
   */
  removable?: boolean;
}): JSX.Element {
  const columns: Column<SourceRow>[] = [
    {
      key: "source",
      header: sourceHeader,
      render: (row) => (
        <div className="min-w-0">
          <Text className="truncate font-medium">{row.label}</Text>
          <Text muted small className="truncate">
            {row.detail}
          </Text>
        </div>
      ),
    },
    {
      key: "role",
      header: "Role",
      width: "280px",
      render: (row) => <RolePicker row={row} roles={roles} />,
    },
    {
      key: "status",
      header: "",
      width: "64px",
      render: (row) =>
        removable && row.mapping ? (
          <RemoveMappingButton mapping={row.mapping} label={row.label} />
        ) : (
          <MappedMark mapped={row.mapping !== undefined} />
        ),
    },
  ];

  return (
    <Table
      columns={columns}
      data={rows}
      rowKey={(row) => row.key}
      // Mapped rows get a light fill so the unmapped ones, still waiting for a
      // role, stand out; unmapped rows keep a lighter hover so hovering one
      // never makes it look mapped.
      renderRow={(row, rowElement) =>
        cloneElement(rowElement as ReactElement<{ className?: string }>, {
          className: cn(
            (rowElement.props as { className?: string }).className,
            row.mapping ? "bg-muted/25 hover:bg-muted/40" : "hover:bg-muted/10",
          ),
        })
      }
      // A scrollable table keeps its header pinned. Its scrollbar is hidden so
      // it never sits beside an open role picker's scrollbar; the cut-off last
      // row shows there is more.
      className={cn(
        scrollable &&
          "[&_thead]:bg-card max-h-[480px] overflow-y-auto [scrollbar-width:none] [&_thead]:sticky [&_thead]:top-0 [&_thead]:z-10 [&::-webkit-scrollbar]:hidden",
      )}
      noResultsMessage={<Text muted>{noResults}</Text>}
    />
  );
}

/**
 * The escape hatch for directories whose groups don't match how access should
 * be split: give a role to everyone with an attribute value, such as a
 * department. It opens from the footer bar under the groups table and starts
 * collapsed, so groups stay the primary path.
 */
function AttributeMappings({
  data,
  roles,
}: {
  data: ListDirectoryRoleMappingsResult;
  roles: Role[];
}): JSX.Element {
  const rows = attributeMappingRows(data);
  const [attributeKey, setAttributeKey] = useState("");
  const [attributeValue, setAttributeValue] = useState("");

  const keys = useMemo(
    () => [...new Set(data.attributes.map((a) => a.key))].sort(),
    [data.attributes],
  );
  const values = data.attributes.filter((a) => a.key === attributeKey);

  const draft: SourceRow = {
    key: "draft",
    label: "",
    detail: "",
    form: { sourceKind: "attribute", attributeKey, attributeValue },
    mapping: undefined,
  };

  return (
    <>
      <Text muted small>
        For roles your groups don&apos;t line up with: give a role to everyone
        with an attribute value, such as a department.
      </Text>

      <div>
        {rows.length > 0 && (
          <MappingTable
            rows={rows}
            roles={roles}
            sourceHeader="Attribute value"
            removable
            noResults="No attribute mappings"
          />
        )}

        {/* The add row sits under the table and lines up with its columns:
          attribute and value take the source column, then the role. */}
        <div
          className={cn(
            "border-border flex items-center gap-3 border px-4 py-3",
            rows.length > 0 && "border-t-0",
          )}
        >
          <Select
            value={attributeKey}
            onValueChange={(key) => {
              setAttributeKey(key);
              setAttributeValue("");
            }}
          >
            <SelectTrigger className="min-w-0 flex-1" aria-label="Attribute">
              <SelectValue placeholder="Attribute" />
            </SelectTrigger>
            <SelectContent>
              {keys.map((key) => (
                <SelectItem key={key} value={key}>
                  {key}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select
            value={attributeValue}
            onValueChange={setAttributeValue}
            disabled={attributeKey === ""}
          >
            <SelectTrigger className="min-w-0 flex-1" aria-label="Value">
              <SelectValue placeholder="Value" />
            </SelectTrigger>
            <SelectContent>
              {values.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.value} ({option.memberCount})
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <div className="w-[280px] shrink-0">
            <RolePicker
              row={draft}
              roles={roles}
              disabledMessage={
                attributeValue === ""
                  ? "Pick an attribute and value first"
                  : undefined
              }
              onSaved={() => {
                setAttributeKey("");
                setAttributeValue("");
              }}
            />
          </div>
          {/* Keeps the role picker in line with the table's mark column. */}
          <div className="w-16 shrink-0" />
        </div>
      </div>
    </>
  );
}

/**
 * Picks the role for one group or attribute value. Picking a role saves it
 * straight away; picking the mapped role again removes the mapping.
 */
function RolePicker({
  row,
  roles,
  disabledMessage,
  onSaved,
}: {
  row: SourceRow;
  roles: Role[];
  disabledMessage?: string;
  onSaved?: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const orgRoutes = useOrgRoutes();

  const refresh = () =>
    Promise.all([
      invalidateAllDirectoryRoleMappings(queryClient),
      invalidateAllRoles(queryClient),
    ]);
  // Returning the refresh keeps the mutation pending until the row reloads.
  const save = useSetDirectoryRoleMappingMutation({
    onSuccess: async () => {
      await refresh();
      onSaved?.();
    },
    onError: (error) => {
      toast.error(errorMessage(error, "Failed to save role mapping"));
    },
  });
  const remove = useDeleteDirectoryRoleMappingMutation({
    onSuccess: () => refresh(),
    onError: (error) => {
      toast.error(errorMessage(error, "Failed to remove role mapping"));
    },
  });
  const saving = save.isPending || remove.isPending;

  const items: DropdownItem[] = roles.map((role) => ({
    value: role.principalUrn,
    label: role.name,
    description: role.description || undefined,
  }));
  items.unshift({
    value: CREATE_ROLE,
    label: "Create role…",
    description: "Opens the role editor",
    icon: <Plus className="h-4 w-4" />,
    separatorAfter: true,
  });

  const pick = (value: string) => {
    if (value === CREATE_ROLE) {
      const params = createRoleForMappingParams(row.form);
      void navigate(`${orgRoutes.createRole.href()}?${params.toString()}`);
      return;
    }
    // Picking the role that is already mapped clears it, like unticking it.
    if (row.mapping && value === row.mapping.roleUrn) {
      remove.mutate({ request: { id: row.mapping.id } });
      return;
    }
    save.mutate({
      request: { setDirectoryRoleMappingForm: { ...row.form, roleUrn: value } },
    });
  };

  const current = roles.find(
    (role) => role.principalUrn === row.mapping?.roleUrn,
  );
  let label = "Assign role";
  if (saving) label = "Saving…";
  else if (row.mapping) label = current?.name ?? "Deleted role";

  return (
    <Combobox
      items={items}
      selected={row.mapping?.roleUrn}
      onSelectionChange={(item) => pick(item.value)}
      searchable
      searchPlaceholder="Search roles…"
      // An unmapped row is a call to action, so its picker is a bordered
      // button; a mapped row reads as settled state.
      variant={row.mapping ? "tertiary" : "secondary"}
      className={cn(
        "h-auto w-full justify-start py-1 text-left",
        !row.mapping && "border-dashed",
      )}
      contentClassName="w-[min(24rem,90vw)]"
      disabledMessage={saving ? "Saving mapping" : disabledMessage}
    >
      <span className="flex items-center gap-2">
        {!row.mapping && !saving && <Plus className="size-4 shrink-0" />}
        {label}
      </span>
    </Combobox>
  );
}

/** The check that marks a group or attribute value as mapped. */
/**
 * Marks a group or attribute value: a check once it gives a role, a warning
 * while it gives its members nothing.
 */
function MappedMark({ mapped }: { mapped: boolean }): JSX.Element {
  return (
    <div className="flex justify-center">
      {mapped ? (
        <Check
          className="text-default-success size-4 shrink-0"
          strokeWidth={2.5}
          aria-label="Mapped"
        />
      ) : (
        <SimpleTooltip tooltip="No role mapped yet. Its members get nothing from it until you assign a role.">
          <span
            role="img"
            aria-label="No role mapped yet. Assign a role to give its members access."
            className="inline-flex"
          >
            <TriangleAlert className="text-warning size-4 shrink-0" />
          </span>
        </SimpleTooltip>
      )}
    </div>
  );
}

/** Removes one mapping. */
function RemoveMappingButton({
  mapping,
  label,
}: {
  mapping: DirectoryRoleMapping;
  label: string;
}): JSX.Element {
  const queryClient = useQueryClient();
  const remove = useDeleteDirectoryRoleMappingMutation({
    onSuccess: () =>
      Promise.all([
        invalidateAllDirectoryRoleMappings(queryClient),
        invalidateAllRoles(queryClient),
      ]),
    onError: (error) => {
      toast.error(errorMessage(error, "Failed to remove role mapping"));
    },
  });

  return (
    <div className="flex justify-center">
      <SimpleTooltip tooltip="Remove this mapping">
        <Button
          variant="tertiary"
          size="sm"
          aria-label={`Remove mapping for ${label}`}
          className="size-8 justify-center p-0"
          disabled={remove.isPending}
          onClick={() => remove.mutate({ request: { id: mapping.id } })}
        >
          {remove.isPending ? (
            <Loader2 className="size-4 animate-spin" />
          ) : (
            <Trash2 className="size-4" />
          )}
        </Button>
      </SimpleTooltip>
    </div>
  );
}

function SyncGroupsButton(): JSX.Element {
  const queryClient = useQueryClient();
  const sync = useSyncDirectoryGroupsMutation({
    onSuccess: async (result) => {
      await invalidateAllDirectoryRoleMappings(queryClient);
      toast.success(`Synced ${result.groupCount} directory groups`);
    },
    onError: (error) => {
      toast.error(errorMessage(error, "Failed to sync directory groups"));
    },
  });

  return (
    <Button
      variant="secondary"
      size="sm"
      // Matches the search box beside it.
      className="h-10"
      disabled={sync.isPending}
      onClick={() => sync.mutate({ request: {} })}
    >
      <Button.LeftIcon>
        {sync.isPending ? (
          <Loader2 className="h-4 w-4 animate-spin" />
        ) : (
          <RefreshCw className="h-4 w-4" />
        )}
      </Button.LeftIcon>
      Sync groups
    </Button>
  );
}
