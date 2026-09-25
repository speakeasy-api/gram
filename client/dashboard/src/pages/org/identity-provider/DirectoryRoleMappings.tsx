import { useQueryClient } from "@tanstack/react-query";
import {
  ChevronRight,
  CircleCheck,
  Loader2,
  Plus,
  RefreshCw,
} from "lucide-react";
import { useDeferredValue, useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router";
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

const UNMAPPED = "__unmapped";
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
export function DirectoryRoleMappings(): JSX.Element {
  const { data, isPending } = useDirectoryRoleMappings();
  const { data: rolesData } = useRoles();
  const [search, setSearch] = useState("");
  const deferredSearch = useDeferredValue(search);

  const roles = useMemo(() => rolesData?.roles ?? [], [rolesData?.roles]);
  const rows = useMemo(() => (data ? groupRows(data) : []), [data]);

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

  const columns: Column<SourceRow>[] = [
    {
      key: "source",
      header: "Directory group",
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
  ];

  return (
    <div className="border-border space-y-3 border-t p-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <div className="text-eyebrow">Role mappings</div>
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

      {isPending && <SkeletonTable />}
      {!isPending && rows.length === 0 && (
        <InlineEmptyState
          icon="users"
          heading="No directory groups yet"
          description="Sync groups to pull them from your directory."
        />
      )}
      {!isPending && rows.length > 0 && (
        <Table
          columns={columns}
          data={visibleRows}
          rowKey={(row) => row.key}
          className="max-h-[480px] overflow-y-auto"
          noResultsMessage={<Text muted>No groups match</Text>}
        />
      )}

      {data && <AttributeMappings data={data} roles={roles} />}
    </div>
  );
}

/**
 * The escape hatch for directories whose groups don't match how access should
 * be split: give a role to everyone with an attribute value, such as a
 * department. Starts collapsed so groups stay the primary path.
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
    <Collapsible className="border-border border-t pt-3">
      <CollapsibleTrigger className="group text-muted-foreground hover:text-foreground flex items-center gap-1.5 text-sm">
        <ChevronRight className="size-4 transition-transform group-data-[state=open]:rotate-90" />
        Attribute mappings
        {rows.length > 0 && <span>({rows.length})</span>}
      </CollapsibleTrigger>
      <CollapsibleContent className="space-y-2 pt-2">
        <Text muted small>
          For roles your groups don&apos;t line up with: give a role to everyone
          with an attribute value, such as a department.
        </Text>

        {rows.map((row) => (
          <div key={row.key} className="flex items-center gap-3">
            <div className="min-w-0 flex-1">
              <Text className="truncate">{row.label}</Text>
              <Text muted small>
                {row.detail}
              </Text>
            </div>
            <div className="w-[280px] shrink-0">
              <RolePicker row={row} roles={roles} />
            </div>
          </div>
        ))}

        <div className="flex flex-wrap items-center gap-2">
          <Select
            value={attributeKey}
            onValueChange={(key) => {
              setAttributeKey(key);
              setAttributeValue("");
            }}
          >
            <SelectTrigger className="w-48" aria-label="Attribute">
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
            <SelectTrigger className="w-56" aria-label="Value">
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
          <div className="w-[280px]">
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
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}

/**
 * Picks the role for one group or attribute value. Picking a role saves it
 * straight away; "Not mapped" removes the mapping.
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
  if (row.mapping) {
    items.unshift({
      value: UNMAPPED,
      label: "Not mapped",
      description: "Remove this mapping",
    });
  }
  items.unshift({
    value: CREATE_ROLE,
    label: "Create role…",
    description: "Opens the role editor",
    icon: <Plus className="h-4 w-4" />,
  });

  const pick = (value: string) => {
    if (value === CREATE_ROLE) {
      void navigate(orgRoutes.createRole.href());
      return;
    }
    if (value === (row.mapping?.roleUrn ?? UNMAPPED)) return;
    if (value === UNMAPPED) {
      if (row.mapping) remove.mutate({ request: { id: row.mapping.id } });
      return;
    }
    save.mutate({
      request: { setDirectoryRoleMappingForm: { ...row.form, roleUrn: value } },
    });
  };

  const current = roles.find(
    (role) => role.principalUrn === row.mapping?.roleUrn,
  );
  let label = "Not mapped";
  if (saving) label = "Saving…";
  else if (row.mapping) label = current?.name ?? "Deleted role";
  else if (row.key === "draft") label = "Pick a role";

  return (
    <Combobox
      items={items}
      selected={row.mapping?.roleUrn}
      onSelectionChange={(item) => pick(item.value)}
      searchable
      searchPlaceholder="Search roles…"
      variant="tertiary"
      className="h-auto w-full justify-start py-1 text-left"
      contentClassName="w-[min(24rem,90vw)]"
      disabledMessage={saving ? "Saving mapping" : disabledMessage}
    >
      <span
        className={cn(
          "flex items-center gap-2",
          !row.mapping && "text-muted-foreground font-normal",
        )}
      >
        {row.mapping && !saving && (
          <CircleCheck
            className="text-default-success size-4 shrink-0"
            aria-label="Mapped"
          />
        )}
        {label}
      </span>
    </Combobox>
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
