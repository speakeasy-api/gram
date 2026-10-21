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
  WandSparkles,
} from "lucide-react";
import {
  cloneElement,
  type ReactElement,
  type ReactNode,
  useDeferredValue,
  useEffect,
  useLayoutEffect,
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
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
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
import { pluralize } from "@/lib/format";
import { cn } from "@/lib/utils";
import { attributeMemberCount } from "@/pages/access/directorySourcesForRole";
import { memberInDirectoryGroup } from "@/lib/member-roles";
import { useOrgRoutes } from "@/routes";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { DirectoryRoleMapping } from "@gram/client/models/components/directoryrolemapping.js";
import type { ListDirectoryRoleMappingsResult } from "@gram/client/models/components/listdirectoryrolemappingsresult.js";
import type { Role } from "@gram/client/models/components/role.js";
import {
  mutationKeyDeleteDirectoryRoleMapping,
  useDeleteDirectoryRoleMappingMutation,
} from "@gram/client/react-query/deleteDirectoryRoleMapping.js";
import { useDirectoryRoleMappings } from "@gram/client/react-query/directoryRoleMappings.js";
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
  type MappingSource,
  pendingMappingFromParams,
  sourceRoleUrns,
  startCreateRoleFlow,
} from "./directoryMappingFlow";

import { invalidateDirectoryMappingAccess } from "./invalidateDirectoryMappingAccess";
import { visibleChipCount } from "./chipOverflow";
import { DirectoryReviewEmailSetting } from "./DirectoryReviewEmailSetting";
import { DirectoryReviewPanel } from "./DirectoryReviewPanel";
import { DirectoryRoleSetupDialog } from "./DirectoryRoleSetupDialog";

const CREATE_ROLE = "__create_role";

// Roles take most of the row so more of a source's role set fits on its one
// line. The attribute add row's grid-cols class repeats this split (1fr 2fr,
// then the mark column) to line up with the table.
const SOURCE_COLUMN = "1fr";
const ROLES_COLUMN = "2fr";
// Matches gap-2 between role chips.
const CHIP_GAP = 8;

/** One directory group or attribute value and the roles it grants. */
type SourceRow = {
  key: string;
  label: string;
  detail: string;
  /** Speakeasy members in the source, shown as faces in place of the detail. */
  members?: FacepileMember[];
  form: MappingSource;
  /** Every live mapping from this source, one per role it grants. */
  mappings: DirectoryRoleMapping[];
  /**
   * The source is gone: a group deleted from the directory, or an attribute
   * value no directory user has. Its mappings grant nothing, so it can only
   * have roles removed, not added.
   */
  missing?: boolean;
};

/** Why a missing source's row cannot take another role. */
function missingSourceMessage(row: SourceRow): string {
  return row.form.sourceKind === "group"
    ? "This group was deleted from the directory. Its roles grant nothing until it returns. Remove them, or keep them for when it does."
    : "No directory user has this value now. Its roles grant nothing until someone does. Remove them, or keep them for when they do.";
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback;
}

/** Undefined means the count is not known. */
function memberLabel(count: number | undefined): string {
  return count === undefined ? "—" : pluralize(count, "user");
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
  const live = data.groups.map((group): SourceRow => ({
    key: group.id,
    label: group.name,
    detail: memberLabel(group.memberCount),
    // Matched by group ID: two directories can each have a group with the
    // same name.
    members: members
      .filter((member) => memberInDirectoryGroup(member, group))
      .map((member) => ({
        id: member.id,
        name: member.name,
        email: member.email,
        photoUrl: member.photoUrl,
      })),
    form: { sourceKind: "group", directoryGroupId: group.id },
    mappings: byGroup.get(group.id) ?? [],
    missing: (byGroup.get(group.id) ?? []).some(
      (mapping) => mapping.sourceStatus === "missing",
    ),
  }));
  // A group deleted from the directory drops out of the group list but keeps
  // its mappings, which now grant nothing. List them so they can be removed.
  const liveIDs = new Set(data.groups.map((group) => group.id));
  const missing = [...byGroup.entries()]
    .filter(([groupId]) => !liveIDs.has(groupId))
    .map(([groupId, mappings]): SourceRow => ({
      key: groupId,
      label: mappings[0]?.directoryGroupName || "Removed group",
      detail: "Deleted from the directory",
      form: { sourceKind: "group", directoryGroupId: groupId },
      mappings,
      missing: true,
    }));
  return [...live, ...missing];
}

/** Attribute mappings that exist, including ones whose value no user has now. */
function attributeMappingRows(
  data: ListDirectoryRoleMappingsResult,
): SourceRow[] {
  const rows = new Map<string, SourceRow>();
  for (const mapping of data.mappings) {
    if (mapping.sourceKind !== "attribute") continue;
    const key = mapping.attributeKey ?? "";
    const value = mapping.attributeValue ?? "";
    const sourceKey = JSON.stringify([key, value]);
    const missing = mapping.sourceStatus === "missing";
    const existing = rows.get(sourceKey);
    if (existing) {
      existing.mappings.push(mapping);
      existing.missing = existing.missing || missing;
      continue;
    }
    rows.set(sourceKey, {
      key: sourceKey,
      label: `${key} = ${value}`,
      detail: missing
        ? "No directory user has this value"
        : memberLabel(attributeMemberCount(data, key, value)),
      form: {
        sourceKind: "attribute",
        attributeKey: key,
        attributeValue: value,
      },
      mappings: [mapping],
      missing,
    });
  }
  return [...rows.values()].sort((a, b) => a.label.localeCompare(b.label));
}

/**
 * Maps directory groups to Speakeasy roles, with attribute values as a secondary
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
  const [settingUp, setSettingUp] = useState(false);

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
  // Wait for fresh mappings so the save is guarded by the source's current
  // roles: a source changed while this tab was away is refused, not replaced.
  const pending =
    data && isFetchedAfterMount && !isFetching && !isError
      ? pendingMappingFromParams(params, data.mappings)
      : undefined;
  const queryClient = useQueryClient();
  const savePending = useSetDirectoryRoleMappingsMutation({
    retry: false,
    onError: (error) => {
      toast.error(errorMessage(error, "Failed to map the new role"));
      void refetch();
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
    // Unmapped groups first, then deleted groups that still have roles, then
    // mapped groups.
    const rank = (row: SourceRow) =>
      row.missing
        ? 1
        : (mappedAtLoad[row.key] ?? row.mappings.length > 0)
          ? 2
          : 0;
    return rows
      .filter(
        (row) => needle === "" || row.label.toLowerCase().includes(needle),
      )
      .sort((a, b) => rank(a) - rank(b) || a.label.localeCompare(b.label));
  }, [rows, deferredSearch, mappedAtLoad]);

  const liveRows = rows.filter((row) => !row.missing);
  const mappedCount = liveRows.filter((row) => row.mappings.length > 0).length;
  const missingCount = rows.length - liveRows.length;
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
            {mappedCount} of {liveRows.length} groups give their members roles.
            {missingCount > 0 &&
              ` ${pluralize(missingCount, "deleted group")} still ${missingCount === 1 ? "has" : "have"} roles mapped.`}
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
          {!isPending && !isError && (
            <Button
              variant="secondary"
              size="sm"
              // Matches the search box beside it.
              className="h-10"
              onClick={() => setSettingUp(true)}
            >
              <Button.LeftIcon>
                <WandSparkles className="h-4 w-4" />
              </Button.LeftIcon>
              Set up in bulk
            </Button>
          )}
          <SyncGroupsButton />
        </div>
      </div>
      {settingUp && (
        <DirectoryRoleSetupDialog
          roles={roles}
          onClose={() => setSettingUp(false)}
        />
      )}
      <DirectoryReviewPanel
        roles={roles}
        rolesLoaded={rolesData !== undefined}
      />
      <DirectoryReviewEmailSetting />

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
      width: SOURCE_COLUMN,
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
      width: ROLES_COLUMN,
      render: (row) => (
        <RolePicker row={row} roles={roles} refreshMappings={refreshMappings} />
      ),
    },
    {
      key: "status",
      header: "",
      width: "64px",
      render: (row) =>
        row.missing ? (
          <MissingSourceMark message={missingSourceMessage(row)} />
        ) : (
          <MappedMark mapped={row.mappings.length > 0} />
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
            row.missing
              ? "text-muted-foreground hover:bg-muted/10"
              : row.mappings.length > 0
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
          attribute and value take the source column, then the role, then
          the mark column's width. */}
        <div
          className={cn(
            "border-border grid grid-cols-[1fr_2fr_4rem] items-center gap-3 border px-4 py-3",
            rows.length > 0 && "border-t-0",
          )}
        >
          <div className="flex min-w-0 items-center gap-3">
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
          </div>
          <div className="min-w-0">
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
    retry: false,
    onSuccess: async () => {
      await invalidateDirectoryMappingAccess(queryClient);
      onSaved?.();
    },
    onError: (error) => {
      toast.error(errorMessage(error, "Failed to save role mapping"));
      void invalidateDirectoryMappingAccess(queryClient);
    },
  });
  const remove = useDeleteDirectoryRoleMappingMutation({
    retry: false,
    onSuccess: async () => {
      await invalidateDirectoryMappingAccess(queryClient);
      onSaved?.();
    },
    onError: (error) => {
      toast.error(errorMessage(error, "Failed to remove role mapping"));
      void invalidateDirectoryMappingAccess(queryClient);
    },
  });
  const deleting =
    useIsMutating({ mutationKey: mutationKeyDeleteDirectoryRoleMapping() }) > 0;
  const setting =
    useIsMutating({ mutationKey: mutationKeySetDirectoryRoleMappings() }) > 0;
  const refreshing =
    useIsFetching({
      queryKey: ["@gram/client", "access", "listDirectoryRoleMappings"],
    }) > 0;
  const [preparing, setPreparing] = useState(false);
  const saving =
    preparing ||
    refreshing ||
    save.isPending ||
    remove.isPending ||
    setting ||
    deleting;
  const changeRole = async (roleUrn: string) => {
    setPreparing(true);
    try {
      const current = sourceRoleUrns(await refreshMappings(), row.form);
      save.mutate({
        request: {
          setDirectoryRoleMappingsForm: {
            ...row.form,
            expectedRoleUrns: current,
            roleUrns: [...new Set([...current, roleUrn])],
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
  const chips = row.mappings.map((mapping) => ({
    id: mapping.id,
    name:
      roles.find((role) => role.principalUrn === mapping.roleUrn)?.name ??
      "Deleted role",
  }));
  return (
    <RoleChips
      chips={chips}
      // The attribute add row's draft has no label; name its chips after the
      // picked attribute value instead.
      sourceLabel={
        row.label ||
        (row.form.sourceKind === "attribute"
          ? `${row.form.attributeKey ?? ""} = ${row.form.attributeValue ?? ""}`
          : "")
      }
      disabled={saving || !!disabledMessage}
      onRemove={(id) => remove.mutate({ request: { id } })}
    >
      {/* A missing source refuses new roles, so it has no add control; its
        current roles stay removable, and the row's mark says why. */}
      {!row.missing && (
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
      )}
    </RoleChips>
  );
}

type RoleChipItem = { id: string; name: string };

/**
 * A source's roles on one line, so a row keeps the same height however many
 * roles the source grants. The chips that fit are shown in order; the rest
 * collapse into a "+N more" chip that opens the whole set, each role still
 * removable there. `children` (the add control) always stays visible after
 * the chips. Until the line can be measured (no layout yet, or in tests)
 * every chip is shown.
 */
function RoleChips({
  chips,
  sourceLabel,
  disabled,
  onRemove,
  children,
}: {
  chips: RoleChipItem[];
  sourceLabel: string;
  disabled: boolean;
  onRemove: (id: string) => void;
  children: ReactNode;
}): JSX.Element {
  const lineRef = useRef<HTMLDivElement>(null);
  const addRef = useRef<HTMLDivElement>(null);
  const measureRef = useRef<HTMLDivElement>(null);
  const popoverRef = useRef<HTMLDivElement>(null);
  const [canMeasure, setCanMeasure] = useState(false);
  const [visible, setVisible] = useState<number | null>(null);
  const [open, setOpen] = useState(false);
  const chipKey = chips.map((chip) => `${chip.id}:${chip.name}`).join("|");

  useLayoutEffect(() => {
    const line = lineRef.current;
    if (!line) return;
    const measure = () => {
      if (line.clientWidth === 0) {
        setCanMeasure(false);
        setVisible(null);
        return;
      }
      setCanMeasure(true);
      const layer = measureRef.current;
      if (!layer) return;
      const sizes = [...layer.children].map(
        (child) => (child as HTMLElement).offsetWidth,
      );
      // The layer holds every chip, then a sample overflow chip.
      const overflowWidth = sizes.pop() ?? 0;
      const addWidth = addRef.current?.offsetWidth ?? 0;
      const available = line.clientWidth - addWidth - CHIP_GAP;
      const next = visibleChipCount(sizes, available, CHIP_GAP, overflowWidth);
      setVisible((previous) => (previous === next ? previous : next));
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(line);
    // The add control changes width while it saves.
    if (addRef.current) observer.observe(addRef.current);
    // Chips can change width when the web font lands, so measure again then.
    let live = true;
    void document.fonts?.ready.then(() => live && measure());
    return () => {
      live = false;
      observer.disconnect();
    };
  }, [chipKey, canMeasure]);

  const shown = visible === null ? chips : chips.slice(0, visible);
  const hidden = chips.length - shown.length;
  // Once every role fits again the overflow chip goes, and so does its
  // popover; reset it so it doesn't reopen when roles overflow later.
  if (hidden === 0 && open) setOpen(false);

  return (
    // Fills the cell, so its width is the room available and not its own
    // content: sized to its content, a line that collapsed to "+N more" would
    // measure too narrow to ever show its chips again.
    <div
      ref={lineRef}
      className="relative flex h-8 w-full min-w-0 items-center gap-2"
    >
      {shown.map((chip) => (
        <RoleChip
          key={chip.id}
          chip={chip}
          sourceLabel={sourceLabel}
          disabled={disabled}
          onRemove={onRemove}
        />
      ))}
      {hidden > 0 && (
        <Popover open={open} onOpenChange={setOpen}>
          <PopoverTrigger asChild>
            <Button
              variant="secondary"
              size="sm"
              className="h-8 shrink-0 px-2"
              aria-label={`Show all ${chips.length} roles from ${sourceLabel}`}
            >
              {overflowLabel(hidden, shown.length)}
            </Button>
          </PopoverTrigger>
          <PopoverContent
            ref={popoverRef}
            tabIndex={-1}
            align="start"
            className="w-[min(24rem,90vw)] p-3"
            // Focus the panel, not its first remove button, so opening it
            // doesn't pop that button's tooltip over the heading; Tab still
            // moves straight into the roles.
            onOpenAutoFocus={(event) => {
              event.preventDefault();
              popoverRef.current?.focus();
            }}
          >
            <Text small muted className="mb-2">
              {pluralize(chips.length, "role")} from {sourceLabel}
            </Text>
            <div className="flex max-h-64 flex-wrap gap-2 overflow-y-auto">
              {chips.map((chip) => (
                <RoleChip
                  key={chip.id}
                  chip={chip}
                  sourceLabel={sourceLabel}
                  disabled={disabled}
                  onRemove={onRemove}
                />
              ))}
            </div>
          </PopoverContent>
        </Popover>
      )}
      <div ref={addRef} className="shrink-0">
        {children}
      </div>
      {/* An invisible copy of every chip, plus the widest overflow chip, so
        the line can work out how many fit without showing them first. */}
      {canMeasure && (
        // Clipped to the line so the copy never widens a scrolling table.
        <div
          aria-hidden
          className="pointer-events-none invisible absolute inset-0 overflow-hidden"
        >
          <div ref={measureRef} className="flex w-max gap-2 whitespace-nowrap">
            {chips.map((chip) => (
              <RoleChip key={chip.id} chip={chip} sourceLabel="" placeholder />
            ))}
            <span className="inline-flex h-8 items-center px-2 text-sm">
              {overflowLabel(chips.length, 1)}
            </span>
          </div>
        </div>
      )}
    </div>
  );
}

function overflowLabel(hidden: number, shown: number): string {
  return shown === 0 ? pluralize(hidden, "role") : `+${hidden} more`;
}

/**
 * One role a source grants, with its remove control. A placeholder chip has
 * the same size but no control, for measuring.
 */
function RoleChip({
  chip,
  sourceLabel,
  disabled = false,
  onRemove,
  placeholder = false,
}: {
  chip: RoleChipItem;
  sourceLabel: string;
  disabled?: boolean;
  onRemove?: (id: string) => void;
  placeholder?: boolean;
}): JSX.Element {
  return (
    <span className="bg-muted/40 border-border inline-flex h-8 max-w-48 shrink-0 items-center gap-1 rounded-md border pl-2 text-sm">
      <span className="truncate" title={chip.name}>
        {chip.name}
      </span>
      {placeholder ? (
        <span className="size-8 shrink-0" />
      ) : (
        <RemoveMappingButton
          label={`${chip.name} from ${sourceLabel}`}
          disabled={disabled}
          onRemove={() => onRemove?.(chip.id)}
        />
      )}
    </span>
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

/** Marks a source that is gone, so its roles grant nothing. */
function MissingSourceMark({ message }: { message: string }): JSX.Element {
  return (
    <div className="flex justify-center">
      <SimpleTooltip tooltip={message}>
        <span role="img" aria-label={message} className="inline-flex">
          <TriangleAlert className="text-destructive size-4 shrink-0" />
        </span>
      </SimpleTooltip>
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
      // A sync also reconciles group memberships, which changes members'
      // groups and mapped roles, not just the group list.
      await invalidateDirectoryMappingAccess(queryClient);
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
