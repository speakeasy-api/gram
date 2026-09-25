import { useQueryClient } from "@tanstack/react-query";
import { Loader2, RefreshCw, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";

import { InlineEmptyState } from "@/components/inline-empty-state";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
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

type SourceKind = "group" | "attribute";

/** What a mapping matches, as a person reads it: a group name or key = value. */
function mappingSourceLabel(mapping: DirectoryRoleMapping): string {
  if (mapping.sourceKind === "group") {
    return mapping.directoryGroupName ?? "Deleted group";
  }
  return `${mapping.attributeKey} = ${mapping.attributeValue}`;
}

/** How many directory users the mapping's group or attribute value covers. */
function mappingSourceMemberCount(
  mapping: DirectoryRoleMapping,
  options: ListDirectoryRoleMappingsResult,
): number | undefined {
  if (mapping.sourceKind === "group") {
    return options.groups.find((g) => g.id === mapping.directoryGroupId)
      ?.memberCount;
  }
  return options.attributes.find(
    (a) => a.key === mapping.attributeKey && a.value === mapping.attributeValue,
  )?.memberCount;
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback;
}

/**
 * Maps directory groups and attribute values to Gram roles. Members who match
 * a mapping get its role on top of the roles assigned to them directly.
 */
export function DirectoryRoleMappings(): JSX.Element {
  const { data, isPending } = useDirectoryRoleMappings();
  const { data: rolesData } = useRoles();
  const [editing, setEditing] = useState<DirectoryRoleMapping | "new" | null>(
    null,
  );

  const roles = useMemo(() => rolesData?.roles ?? [], [rolesData?.roles]);
  const roleByUrn = useMemo(
    () => new Map(roles.map((role) => [role.principalUrn, role])),
    [roles],
  );

  const columns: Column<DirectoryRoleMapping>[] = [
    {
      key: "source",
      header: "Directory",
      render: (mapping) => (
        <div className="min-w-0">
          <Text className="truncate font-medium">
            {mappingSourceLabel(mapping)}
          </Text>
          <Text muted small>
            {mapping.sourceKind === "group" ? "Group" : "Attribute"}
          </Text>
        </div>
      ),
    },
    {
      key: "users",
      header: "Users",
      width: "90px",
      render: (mapping) => (
        <Text>
          {data ? (mappingSourceMemberCount(mapping, data) ?? "—") : "—"}
        </Text>
      ),
    },
    {
      key: "role",
      header: "Role",
      width: "200px",
      render: (mapping) => (
        <Text>{roleByUrn.get(mapping.roleUrn)?.name ?? "Deleted role"}</Text>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "150px",
      render: (mapping) => (
        <RequireScope scope="org:admin" level="component">
          <div className="flex justify-end gap-1">
            <Button
              variant="tertiary"
              size="sm"
              onClick={() => setEditing(mapping)}
            >
              Edit
            </Button>
            <DeleteMappingButton mapping={mapping} />
          </div>
        </RequireScope>
      ),
    },
  ];

  const mappings = data?.mappings ?? [];

  return (
    <div className="border-border border-t">
      <div className="flex items-center justify-between gap-4 px-4 pt-4 pb-3">
        <div>
          <div className="text-eyebrow">Role mappings</div>
          <Text muted small>
            Give everyone in a directory group, or with a directory attribute, a
            role.
          </Text>
        </div>
        <RequireScope scope="org:admin" level="component">
          <div className="flex shrink-0 gap-2">
            <SyncGroupsButton />
            <Button size="sm" onClick={() => setEditing("new")}>
              Add mapping
            </Button>
          </div>
        </RequireScope>
      </div>

      {isPending && <SkeletonTable />}
      {!isPending && mappings.length === 0 && (
        <InlineEmptyState
          icon="users"
          heading="No role mappings yet"
          description="Members keep only the roles assigned to them directly until you map a group or attribute."
          className="px-4 pb-4"
        />
      )}
      {!isPending && mappings.length > 0 && (
        <Table columns={columns} data={mappings} rowKey={(row) => row.id} />
      )}

      {data && (
        <MappingDialog
          editing={editing}
          options={data}
          roles={roles}
          onOpenChange={(open) => {
            if (!open) setEditing(null);
          }}
        />
      )}
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

function DeleteMappingButton({
  mapping,
}: {
  mapping: DirectoryRoleMapping;
}): JSX.Element {
  const queryClient = useQueryClient();
  const remove = useDeleteDirectoryRoleMappingMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllDirectoryRoleMappings(queryClient),
        invalidateAllRoles(queryClient),
      ]);
      toast.success("Role mapping removed");
    },
    onError: (error) => {
      toast.error(errorMessage(error, "Failed to remove role mapping"));
    },
  });

  return (
    <Button
      variant="tertiary"
      size="sm"
      aria-label={`Remove mapping for ${mappingSourceLabel(mapping)}`}
      disabled={remove.isPending}
      onClick={() => remove.mutate({ request: { id: mapping.id } })}
    >
      <Trash2 className="h-4 w-4" />
    </Button>
  );
}

function MappingDialog({
  editing,
  options,
  roles,
  onOpenChange,
}: {
  editing: DirectoryRoleMapping | "new" | null;
  options: ListDirectoryRoleMappingsResult;
  roles: Role[];
  onOpenChange: (open: boolean) => void;
}): JSX.Element {
  const existing = editing === "new" ? null : editing;
  // Keyed on the mapping so each open starts from that mapping's values.
  const formKey = existing?.id ?? "new";

  return (
    <Dialog open={editing !== null} onOpenChange={onOpenChange}>
      <Dialog.Content className="sm:max-w-md">
        <Dialog.Header>
          <Dialog.Title>
            {existing ? "Edit role mapping" : "Add role mapping"}
          </Dialog.Title>
          <Dialog.Description>
            Members who match get this role on top of the roles assigned to them
            directly.
          </Dialog.Description>
        </Dialog.Header>
        {editing !== null && (
          <MappingForm
            key={formKey}
            existing={existing}
            options={options}
            roles={roles}
            onDone={() => onOpenChange(false)}
          />
        )}
      </Dialog.Content>
    </Dialog>
  );
}

function MappingForm({
  existing,
  options,
  roles,
  onDone,
}: {
  existing: DirectoryRoleMapping | null;
  options: ListDirectoryRoleMappingsResult;
  roles: Role[];
  onDone: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const [sourceKind, setSourceKind] = useState<SourceKind>(
    existing?.sourceKind ?? "group",
  );
  const [groupId, setGroupId] = useState(existing?.directoryGroupId ?? "");
  const [attributeKey, setAttributeKey] = useState(
    existing?.attributeKey ?? "",
  );
  const [attributeValue, setAttributeValue] = useState(
    existing?.attributeValue ?? "",
  );
  const [roleUrn, setRoleUrn] = useState(existing?.roleUrn ?? "");

  const attributeKeys = useMemo(
    () => [...new Set(options.attributes.map((a) => a.key))],
    [options.attributes],
  );
  const attributeValues = options.attributes.filter(
    (a) => a.key === attributeKey,
  );

  const save = useSetDirectoryRoleMappingMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllDirectoryRoleMappings(queryClient),
        invalidateAllRoles(queryClient),
      ]);
      toast.success("Role mapping saved");
      onDone();
    },
    onError: (error) => {
      toast.error(errorMessage(error, "Failed to save role mapping"));
    },
  });

  const sourceChosen =
    sourceKind === "group" ? groupId !== "" : attributeValue !== "";
  const canSave = sourceChosen && roleUrn !== "" && !save.isPending;

  const submit = () => {
    const form: SetDirectoryRoleMappingForm = { sourceKind, roleUrn };
    if (sourceKind === "group") {
      form.directoryGroupId = groupId;
    } else {
      form.attributeKey = attributeKey;
      form.attributeValue = attributeValue;
    }
    save.mutate({ request: { setDirectoryRoleMappingForm: form } });
  };

  return (
    <div className="space-y-4 py-2">
      {/* The source is the mapping's identity: editing changes only its role. */}
      <SegmentedControl<SourceKind>
        value={sourceKind}
        onChange={setSourceKind}
        disabled={existing !== null}
        options={[
          { value: "group", label: "Group" },
          { value: "attribute", label: "Attribute" },
        ]}
      />

      {sourceKind === "group" && (
        <LabeledSelect
          label="Directory group"
          placeholder={
            options.groups.length === 0
              ? "No groups synced yet"
              : "Pick a group"
          }
          value={groupId}
          onChange={setGroupId}
          disabled={existing !== null}
          items={options.groups.map((g) => ({
            value: g.id,
            label: `${g.name} (${g.memberCount})`,
          }))}
        />
      )}

      {sourceKind === "attribute" && (
        <>
          <LabeledSelect
            label="Attribute"
            placeholder="Pick an attribute"
            value={attributeKey}
            onChange={(key) => {
              setAttributeKey(key);
              setAttributeValue("");
            }}
            disabled={existing !== null}
            items={attributeKeys.map((key) => ({ value: key, label: key }))}
          />
          <LabeledSelect
            label="Value"
            placeholder="Pick a value"
            value={attributeValue}
            onChange={setAttributeValue}
            disabled={existing !== null || attributeKey === ""}
            items={attributeValues.map((a) => ({
              value: a.value,
              label: `${a.value} (${a.memberCount})`,
            }))}
          />
        </>
      )}

      <LabeledSelect
        label="Role"
        placeholder="Pick a role"
        value={roleUrn}
        onChange={setRoleUrn}
        items={roles.map((role) => ({
          value: role.principalUrn,
          label: role.name,
        }))}
      />

      <Dialog.Footer>
        <Button variant="secondary" onClick={onDone}>
          Cancel
        </Button>
        <Button disabled={!canSave} onClick={submit}>
          {save.isPending && (
            <Button.LeftIcon>
              <Loader2 className="h-4 w-4 animate-spin" />
            </Button.LeftIcon>
          )}
          Save
        </Button>
      </Dialog.Footer>
    </div>
  );
}

function LabeledSelect({
  label,
  placeholder,
  value,
  onChange,
  items,
  disabled,
}: {
  label: string;
  placeholder: string;
  value: string;
  onChange: (value: string) => void;
  items: { value: string; label: string }[];
  disabled?: boolean;
}): JSX.Element {
  return (
    <div className="space-y-1.5">
      <div className="text-eyebrow">{label}</div>
      <Select value={value} onValueChange={onChange} disabled={disabled}>
        <SelectTrigger className="w-full">
          <SelectValue placeholder={placeholder} />
        </SelectTrigger>
        <SelectContent>
          {items.map((item) => (
            <SelectItem key={item.value} value={item.value}>
              {item.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
