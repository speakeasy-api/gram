import {
  useIsFetching,
  useIsMutating,
  useQueryClient,
} from "@tanstack/react-query";
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
import {
  type FacepileMember,
  MemberFacepile,
} from "@/components/member-facepile";
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
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { DirectoryRoleMapping } from "@gram/client/models/components/directoryrolemapping.js";
import type { ListDirectoryRoleMappingsResult } from "@gram/client/models/components/listdirectoryrolemappingsresult.js";
import type { Role } from "@gram/client/models/components/role.js";
import type { SetDirectoryRoleMappingsForm } from "@gram/client/models/components/setdirectoryrolemappingsform.js";

import {
  invalidateAllDirectoryRoleMappings,
  useDirectoryRoleMappings,
} from "@gram/client/react-query/directoryRoleMappings.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useRoles } from "@gram/client/react-query/roles.js";
import {
  mutationKeySetDirectoryRoleMappings,
  useSetDirectoryRoleMappingsMutation,
} from "@gram/client/react-query/setDirectoryRoleMappings.js";
import { useSyncDirectoryGroupsMutation } from "@gram/client/react-query/syncDirectoryGroups.js";

import {
  finishCreateRoleFlow,
  availableRoleName,
  pendingMappingFromParams,
  startCreateRoleFlow,
} from "./directoryMappingFlow";

import { invalidateDirectoryMappingAccess } from "./invalidateDirectoryMappingAccess";

const CREATE_ROLE = "__create_role";

/** One directory group or attribute value and the roles it grants. */
type SourceRow = {
  key: string;
  label: string;
  detail: string;
  /** Gram members in the source, shown as faces in place of the detail. */
  members?: FacepileMember[];
  form: Omit<SetDirectoryRoleMappingsForm, "roleUrns">;
  mappings: DirectoryRoleMapping[];
};

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback;
}

function memberLabel(count: number): string {
  return `${count} ${count === 1 ? "user" : "users"}`;
}

function groupRows(
  data: ListDirectoryRoleMappingsResult,
  members: AccessMember[],
): SourceRow[] {
  const byGroup = new Map<string, DirectoryRoleMapping[]>();
  for (const mapping of data.mappings) {
    if (mapping.sourceKind !== "group" || !mapping.directoryGroupId) continue;
    const mappings = byGroup.get(mapping.directoryGroupId) ?? [];
    mappings.push(mapping);
    byGroup.set(mapping.directoryGroupId, mappings);
  }
  // Members name their directory groups, not group ids, so match on the name.
  const membersByGroup = new Map<string, FacepileMember[]>();
  for (const member of members) {
    for (const group of member.groups ?? []) {
      const list = membersByGroup.get(group) ?? [];
      list.push({
        id: member.id,
        name: member.name,
        email: member.email,
        photoUrl: member.photoUrl,
      });
      membersByGroup.set(group, list);
    }
  }
  return data.groups.map((group) => ({
    key: group.id,
    label: group.name,
    detail: memberLabel(group.memberCount),
    members: membersByGroup.get(group.name),
    form: { sourceKind: "group", directoryGroupId: group.id },
    mappings: byGroup.get(group.id) ?? [],
  }));
}

/** Attribute mappings that exist, including ones whose value no user has now. */
function attributeMappingRows(
  data: ListDirectoryRoleMappingsResult,
): SourceRow[] {
  const rows = new Map<string, SourceRow>();
  const attributes = new Map(
    data.attributes.map((attribute) => [
      JSON.stringify([attribute.key, attribute.value]),
      attribute,
    ]),
  );
  for (const mapping of data.mappings) {
    if (mapping.sourceKind !== "attribute") continue;
    const key = mapping.attributeKey ?? "";
    const value = mapping.attributeValue ?? "";
    const sourceKey = JSON.stringify([key, value]);
    const existing = rows.get(sourceKey);
    if (existing) {
      existing.mappings.push(mapping);
      continue;
    }
    const option = attributes.get(sourceKey);
    rows.set(sourceKey, {
      key: sourceKey,
      label: `${key} = ${value}`,
      detail: memberLabel(option?.memberCount ?? 0),
      form: {
        sourceKind: "attribute",
        attributeKey: key,
        attributeValue: value,
      },
      mappings: [mapping],
    });
  }
  return [...rows.values()].sort((a, b) => a.label.localeCompare(b.label));
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
  const { data, isPending, isFetching, isFetchedAfterMount, isError, refetch } =
    useDirectoryRoleMappings(undefined, undefined, {
      refetchOnMount: "always",
    });
  const refreshMappings = async (): Promise<DirectoryRoleMapping[]> => {
    const result = await refetch({ throwOnError: true });
    if (!result.data) throw new Error("Failed to refresh role mappings");
    return result.data.mappings;
  };
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
  const { data: membersData } = useMembers();
  const rows = useMemo(
    () => (data ? groupRows(data, membersData?.members ?? []) : []),
    [data, membersData?.members],
  );

  // Back from creating a role for a group or attribute: map it. The round
  // trip ends only once the save succeeds; a failed save keeps it so a reload
  // retries. Only a round trip this tab started can get here.
  const [params, setParams] = useSearchParams();
  const pending =
    data && isFetchedAfterMount && !isFetching && !isError
      ? pendingMappingFromParams(params, data.mappings)
      : undefined;
  const queryClient = useQueryClient();
  const savePending = useSetDirectoryRoleMappingsMutation({
    onError: (error) => {
      toast.error(errorMessage(error, "Failed to map the new role"));
    },
  });
  const savedPending = useRef<string | null>(null);
  useEffect(() => {
    if (!pending || savedPending.current === pending.key) return;
    savedPending.current = pending.key;
    savePending.mutate(
      { request: { setDirectoryRoleMappingsForm: pending.form } },
      {
        onSuccess: () => {
          setParams((previous) => finishCreateRoleFlow(previous, pending.key), {
            replace: true,
          });
          void invalidateDirectoryMappingAccess(queryClient).then(() =>
            toast.success("Role created and mapped"),
          );
        },
      },
    );
  }, [pending, savePending, setParams, queryClient]);

  // Unmapped groups sort first. Each row's place is fixed by whether it was
  // mapped when it first loaded, so picking a role does not move the row
  // under the cursor; the order refreshes the next time the panel mounts.
  const [mappedAtLoad, setMappedAtLoad] = useState<Record<string, boolean>>({});
  useEffect(() => {
    setMappedAtLoad((previous) => {
      const missing = rows.filter((row) => !(row.key in previous));
      if (missing.length === 0) return previous;
      const next = { ...previous };
      for (const row of missing) next[row.key] = row.mappings.length > 0;
      return next;
    });
  }, [rows]);

  const visibleRows = useMemo(() => {
    const needle = deferredSearch.trim().toLowerCase();
    const wasMapped = (row: SourceRow) =>
      mappedAtLoad[row.key] ?? row.mappings.length > 0;
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

  const mappedCount = rows.filter((row) => row.mappings.length > 0).length;
  const attributeRows = useMemo(
    () => (data ? attributeMappingRows(data) : []),
    [data],
  );
  const attributeCount = attributeRows.length;

  return (
    <div className="mt-4 space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <div className="text-eyebrow">Configure role mappings</div>
          <Text muted small>
            {mappedCount} of {rows.length} groups give their members roles.
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
            refreshMappings={refreshMappings}
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
            <Button
              variant="tertiary"
              size="sm"
              className="group text-muted-foreground hover:text-foreground"
            >
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
          {data && (
            <AttributeMappings
              data={data}
              rows={attributeRows}
              roles={roles}
              refreshMappings={refreshMappings}
            />
          )}
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
  refreshMappings,
  sourceHeader,
  noResults,
  scrollable = false,
}: {
  rows: SourceRow[];
  roles: Role[];
  refreshMappings: () => Promise<DirectoryRoleMapping[]>;
  sourceHeader: string;
  noResults: string;
  scrollable?: boolean;
}): JSX.Element {
  const columns: Column<SourceRow>[] = [
    {
      key: "source",
      header: sourceHeader,
      render: (row) => (
        <div className="min-w-0">
          <Text className="truncate font-medium">{row.label}</Text>
          {row.members?.length ? (
            <div className="mt-1">
              <MemberFacepile members={row.members} />
            </div>
          ) : (
            <Text muted small className="truncate">
              {row.detail}
            </Text>
          )}
        </div>
      ),
    },
    {
      key: "role",
      header: "Roles",
      width: "280px",
      render: (row) => (
        <RolePicker row={row} roles={roles} refreshMappings={refreshMappings} />
      ),
    },
    {
      key: "status",
      header: "",
      width: "64px",
      render: (row) => <MappedMark mapped={row.mappings.length > 0} />,
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
            row.mappings.length > 0
              ? "bg-muted/25 hover:bg-muted/40"
              : "hover:bg-muted/10",
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
  rows,
  roles,
  refreshMappings,
}: {
  data: ListDirectoryRoleMappingsResult;
  rows: SourceRow[];
  roles: Role[];
  refreshMappings: () => Promise<DirectoryRoleMapping[]>;
}): JSX.Element {
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
    mappings: data.mappings.filter(
      (mapping) =>
        mapping.sourceKind === "attribute" &&
        mapping.attributeKey === attributeKey &&
        mapping.attributeValue === attributeValue,
    ),
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
            refreshMappings={refreshMappings}
            sourceHeader="Attribute value"

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
              refreshMappings={refreshMappings}
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

function RolePicker({
  row,
  roles,
  refreshMappings,
  disabledMessage,
  onSaved,
}: {
  row: SourceRow;
  roles: Role[];
  refreshMappings: () => Promise<DirectoryRoleMapping[]>;
  disabledMessage?: string;
  onSaved?: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const orgRoutes = useOrgRoutes();
  const save = useSetDirectoryRoleMappingsMutation({
    onSuccess: async () => {
      await invalidateDirectoryMappingAccess(queryClient);
      onSaved?.();
    },
    onError: (error) => {
      toast.error(errorMessage(error, "Failed to save role mapping"));
    },
  });
  const setting =
    useIsMutating({ mutationKey: mutationKeySetDirectoryRoleMappings() }) > 0;
  const refreshing =
    useIsFetching({
      queryKey: ["@gram/client", "access", "listDirectoryRoleMappings"],
    }) > 0;
  const [preparing, setPreparing] = useState(false);
  const saving = preparing || refreshing || save.isPending || setting;
  const changeRole = async (roleUrn: string, remove = false) => {
    setPreparing(true);
    try {
      const mappings = await refreshMappings();
      const current = mappings
        .filter((mapping) =>
          row.form.sourceKind === "group"
            ? mapping.sourceKind === "group" &&
              mapping.directoryGroupId === row.form.directoryGroupId
            : mapping.sourceKind === "attribute" &&
              mapping.attributeKey === row.form.attributeKey &&
              mapping.attributeValue === row.form.attributeValue,
        )
        .map((mapping) => mapping.roleUrn);
      save.mutate({
        request: {
          setDirectoryRoleMappingsForm: {
            ...row.form,
            roleUrns: remove
              ? current.filter((role) => role !== roleUrn)
              : [...new Set([...current, roleUrn])],
          },
        },
      });
    } catch (error) {
      toast.error(errorMessage(error, "Failed to refresh role mappings"));
    } finally {
      setPreparing(false);
    }
  };
  const mappedRoles = row.mappings.map((mapping) => mapping.roleUrn);
  const items: DropdownItem[] = roles
    .filter((role) => !mappedRoles.includes(role.principalUrn))
    .map((role) => ({
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
      const baseName =
        row.form.sourceKind === "attribute"
          ? (row.form.attributeValue ?? "")
          : row.label;
      const params = startCreateRoleFlow(
        row.form,
        availableRoleName(baseName, roles),
      );
      void navigate(`${orgRoutes.createRole.href()}?${params.toString()}`);
      return;
    }
    void changeRole(value);
  };
  return (
    <div className="flex flex-wrap items-center gap-2">
      {row.mappings.map((mapping) => {
        const name =
          roles.find((role) => role.principalUrn === mapping.roleUrn)?.name ??
          "Deleted role";
        return (
          <span
            key={mapping.id}
            className="bg-muted/40 border-border inline-flex max-w-full items-center gap-1 rounded-md border pl-2 text-sm"
          >
            <span className="truncate" title={name}>
              {name}
            </span>
            <RemoveMappingButton
              label={`${name} from ${row.label}`}
              disabled={saving || !!disabledMessage}
              onRemove={() => void changeRole(mapping.roleUrn, true)}
            />
          </span>
        );
      })}
      <Combobox
        items={items}
        selected={undefined}
        onSelectionChange={(item) => pick(item.value)}
        searchable
        searchPlaceholder="Search roles…"
        variant="secondary"
        className="h-auto justify-start border-dashed py-1 text-left"
        contentClassName="w-[min(24rem,90vw)]"
        disabledMessage={saving ? "Saving mapping" : disabledMessage}
      >
        <span className="flex items-center gap-2">
          <Plus className="size-4 shrink-0" />
          {saving ? "Saving…" : "Add role"}
        </span>
      </Combobox>
    </div>
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
  label,
  disabled,
  onRemove,
}: {
  label: string;
  disabled: boolean;
  onRemove: () => void;
}): JSX.Element {
  return (
    <div className="flex justify-center">
      <SimpleTooltip tooltip="Remove this role from the source">
        <Button
          variant="tertiary"
          size="sm"
          aria-label={`Remove ${label}`}
          className="size-8 justify-center p-0"
          disabled={disabled}
          onClick={onRemove}
        >
          <Trash2 className="size-4" />
        </Button>
      </SimpleTooltip>
    </div>
  );
}

function SyncGroupsButton(): JSX.Element {
  const queryClient = useQueryClient();
  const sync = useSyncDirectoryGroupsMutation({
    onSuccess: async () => {
      await invalidateAllDirectoryRoleMappings(queryClient);
      toast.success("Updated directory group list");
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
