import { useQueryClient } from "@tanstack/react-query";
import { Loader2, Plus, RefreshCw } from "lucide-react";
import { useDeferredValue, useMemo, useState } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";

import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import { Combobox, type DropdownItem } from "@/components/ui/Combobox";
import { SearchBar } from "@/components/ui/SearchBar";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { type Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { useOrgRoutes } from "@/routes";
import type { DirectoryRoleMapping } from "@gram/client/models/components/directoryrolemapping.js";
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

type SourceKind = "group" | "attribute";
type MappingFilter = "all" | "mapped" | "unmapped";

const UNMAPPED = "__unmapped";
const CREATE_ROLE = "__create_role";

/** One directory group or attribute value that can be mapped to a role. */
type SourceRow = {
  key: string;
  label: string;
  detail: string;
  memberCount: number;
  form: Omit<SetDirectoryRoleMappingForm, "roleUrn">;
  mapping: DirectoryRoleMapping | undefined;
};

function attributeKey(key: string, value: string): string {
  return `${key}\u0000${value}`;
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback;
}

function memberLabel(count: number): string {
  return `${count} ${count === 1 ? "user" : "users"}`;
}

/**
 * Maps directory groups and attribute values to Gram roles. Every group and
 * attribute value is a row with its own role picker; picking a role saves it.
 * Members who match get that role on top of the roles assigned to them
 * directly. Render it only for org admins: the listing exposes directory
 * attribute values and the server rejects anyone else.
 */
export function DirectoryRoleMappings(): JSX.Element {
  const { data, isPending } = useDirectoryRoleMappings();
  const { data: rolesData } = useRoles();
  const [kind, setKind] = useState<SourceKind>("group");
  const [filter, setFilter] = useState<MappingFilter>("all");
  const [search, setSearch] = useState("");
  const deferredSearch = useDeferredValue(search);

  const roles = useMemo(() => rolesData?.roles ?? [], [rolesData?.roles]);

  const rows = useMemo<SourceRow[]>(() => {
    if (!data) return [];
    if (kind === "group") {
      const byGroup = new Map(
        data.mappings
          .filter((m) => m.sourceKind === "group")
          .map((m) => [m.directoryGroupId, m]),
      );
      return data.groups.map((group) => ({
        key: group.id,
        label: group.name,
        detail: memberLabel(group.memberCount),
        memberCount: group.memberCount,
        form: { sourceKind: "group", directoryGroupId: group.id },
        mapping: byGroup.get(group.id),
      }));
    }
    const byAttribute = new Map(
      data.mappings
        .filter((m) => m.sourceKind === "attribute")
        .map((m) => [
          attributeKey(m.attributeKey ?? "", m.attributeValue ?? ""),
          m,
        ]),
    );
    return data.attributes.map((attribute) => ({
      key: attributeKey(attribute.key, attribute.value),
      label: attribute.value,
      detail: `${attribute.key} · ${memberLabel(attribute.memberCount)}`,
      memberCount: attribute.memberCount,
      form: {
        sourceKind: "attribute",
        attributeKey: attribute.key,
        attributeValue: attribute.value,
      },
      mapping: byAttribute.get(attributeKey(attribute.key, attribute.value)),
    }));
  }, [data, kind]);

  const visibleRows = useMemo(() => {
    const needle = deferredSearch.trim().toLowerCase();
    return rows.filter((row) => {
      if (filter === "mapped" && !row.mapping) return false;
      if (filter === "unmapped" && row.mapping) return false;
      if (needle === "") return true;
      return `${row.label} ${row.detail}`.toLowerCase().includes(needle);
    });
  }, [rows, filter, deferredSearch]);

  const mappedCount = rows.filter((row) => row.mapping).length;

  const columns: Column<SourceRow>[] = [
    {
      key: "source",
      header: kind === "group" ? "Directory group" : "Attribute value",
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
            {mappedCount} of {rows.length}{" "}
            {kind === "group" ? "groups" : "attribute values"} give their
            members a role.
          </Text>
        </div>
        {kind === "group" && <SyncGroupsButton />}
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <SegmentedControl<SourceKind>
          value={kind}
          onChange={setKind}
          options={[
            { value: "group", label: "Groups" },
            { value: "attribute", label: "Attributes" },
          ]}
        />
        <SearchBar
          value={search}
          onChange={setSearch}
          placeholder={
            kind === "group" ? "Search groups" : "Search attributes and values"
          }
          className="w-64"
        />
        <SegmentedControl<MappingFilter>
          value={filter}
          onChange={setFilter}
          options={[
            { value: "all", label: "All" },
            { value: "mapped", label: "Mapped" },
            { value: "unmapped", label: "Not mapped" },
          ]}
        />
      </div>

      {isPending && <SkeletonTable />}
      {!isPending && rows.length === 0 && (
        <InlineEmptyState
          icon="users"
          heading={
            kind === "group" ? "No directory groups yet" : "No attributes yet"
          }
          description={
            kind === "group"
              ? "Sync groups to pull them from your directory."
              : "Attributes appear once your directory syncs users with attributes."
          }
        />
      )}
      {!isPending && rows.length > 0 && (
        <Table
          columns={columns}
          data={visibleRows}
          rowKey={(row) => row.key}
          className="max-h-[480px] overflow-y-auto"
          noResultsMessage={<Text muted>Nothing matches</Text>}
        />
      )}
    </div>
  );
}

/**
 * Picks the role for one group or attribute value. Picking a role saves it
 * straight away; "Not mapped" removes the mapping.
 */
function RolePicker({
  row,
  roles,
}: {
  row: SourceRow;
  roles: Role[];
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
    onSuccess: () => refresh(),
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
      disabledMessage={saving ? "Saving mapping" : undefined}
    >
      <span className={row.mapping ? "" : "text-muted-foreground font-normal"}>
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
