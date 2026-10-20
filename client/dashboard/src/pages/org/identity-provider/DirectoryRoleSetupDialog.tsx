import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { Dialog } from "@/components/ui/Dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import { pluralize } from "@/lib/format";
import type { Role } from "@gram/client/models/components/role.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useApplyDirectoryRoleSetupMutation } from "@gram/client/react-query/applyDirectoryRoleSetup.js";
import { useDirectoryRoleSetupPreview } from "@gram/client/react-query/directoryRoleSetupPreview.js";
import { invalidateAllRoles } from "@gram/client/react-query/roles.js";

import {
  choiceFromValue,
  choiceValue,
  MAX_SETUP_ITEMS,
  newRoleNames,
  reusedRoleUrns,
  type SetupRow,
  setupRequest,
  setupResultSummary,
  setupRows,
} from "./directoryRoleSetup";
import { invalidateDirectoryMappingAccess } from "./invalidateDirectoryMappingAccess";

const NO_GRANTS = "__none";

/**
 * Gives every unmapped directory group, and any attribute values picked, a
 * role in one go. The server proposes a role per source: an existing role
 * with the same name, or a new one named after the source. New roles get no
 * grants unless the admin copies another role's grants. Nothing is saved
 * until Apply, and Apply is refused if the directory or its mappings changed
 * since the preview.
 */
export function DirectoryRoleSetupDialog({
  roles,
  onClose,
}: {
  /** Roles sorted for display. */
  roles: Role[];
  onClose: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const preview = useDirectoryRoleSetupPreview(undefined, undefined, {
    refetchOnMount: "always",
    retry: false,
    throwOnError: false,
  });
  const [rows, setRows] = useState<SetupRow[]>([]);
  useEffect(() => {
    if (preview.data) setRows((previous) => setupRows(preview.data, previous));
  }, [preview.data]);
  const [grantSource, setGrantSource] = useState(NO_GRANTS);
  const [changed, setChanged] = useState(false);

  const apply = useApplyDirectoryRoleSetupMutation({
    retry: false,
    onSuccess: async (result, variables) => {
      toast.success(
        setupResultSummary(
          result,
          variables.request.applyDirectoryRoleSetupForm.items.length,
        ),
      );
      onClose();
      await invalidateDirectoryMappingAccess(queryClient);
    },
    onError: (error) => {
      if (error instanceof GramError && error.statusCode === 409) {
        // Someone changed a source or its roles since the preview. Show the
        // current proposals; the admin reviews them and applies again. The
        // role list is refreshed too, so a role created meanwhile is
        // selectable.
        setChanged(true);
        void preview.refetch();
        void invalidateAllRoles(queryClient);
      }
    },
  });

  const roleByUrn = useMemo(
    () => new Map(roles.map((role) => [role.principalUrn, role])),
    [roles],
  );
  const selected = rows.filter((row) => row.selected);
  const creates = newRoleNames(rows);
  const reused = reusedRoleUrns(rows)
    .map((urn) => roleByUrn.get(urn))
    .filter((role): role is Role => !!role && role.grants.length > 0);
  const grantRole = roleByUrn.get(grantSource);

  // Only a preview that is current can be applied.
  const ready = !preview.isFetching && !preview.isError && !!preview.data;
  const tooMany = selected.length > MAX_SETUP_ITEMS;
  const canApply = ready && selected.length > 0 && !tooMany && !apply.isPending;

  const update = (key: string, change: Partial<SetupRow>) =>
    setRows((previous) =>
      previous.map((row) => (row.key === key ? { ...row, ...change } : row)),
    );
  const allSelected = rows.length > 0 && selected.length === rows.length;

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !apply.isPending) onClose();
      }}
    >
      <Dialog.Content className="max-w-3xl">
        <Dialog.Header>
          <Dialog.Title>Set up role mappings</Dialog.Title>
          <Dialog.Description>
            Give directory groups and attribute values that have no role yet a
            role of their own. Each gets an existing role with the same name, or
            a new role named after it. Review the roles, then apply them
            together.
          </Dialog.Description>
        </Dialog.Header>

        {preview.isError && !preview.isFetching ? (
          <Alert variant="error" dismissible={false}>
            <div className="flex items-center gap-3">
              <span className="text-sm">Couldn&apos;t load the proposals.</span>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => void preview.refetch()}
              >
                <Button.Text>Retry</Button.Text>
              </Button>
            </div>
          </Alert>
        ) : !preview.data ? (
          <Text small muted>
            Loading directory groups…
          </Text>
        ) : (
          <div className="space-y-3">
            {changed && (
              <Alert variant="warning" dismissible={false}>
                The directory or its role mappings changed since this preview,
                so nothing was saved. The list below is up to date; review it
                and apply again.
              </Alert>
            )}

            {rows.length === 0 ? (
              <Text small muted>
                Every directory group and attribute value already has a role.
                Change them in the table.
              </Text>
            ) : (
              <>
                <div className="flex items-center justify-between">
                  <Text small muted>
                    {selected.length} of {pluralize(rows.length, "source")}{" "}
                    selected
                  </Text>
                  <Button
                    variant="tertiary"
                    size="sm"
                    onClick={() =>
                      setRows((previous) =>
                        previous.map((row) => ({
                          ...row,
                          selected: !allSelected,
                        })),
                      )
                    }
                  >
                    <Button.Text>
                      {allSelected ? "Clear all" : "Select all"}
                    </Button.Text>
                  </Button>
                </div>
                <ul className="border-border max-h-[360px] divide-y overflow-y-auto border">
                  {rows.map((row) => (
                    <SetupRowItem
                      key={row.key}
                      row={row}
                      roles={roles}
                      roleByUrn={roleByUrn}
                      disabled={apply.isPending}
                      onChange={(change) => update(row.key, change)}
                    />
                  ))}
                </ul>
              </>
            )}

            {preview.data.hiddenAttributeKeys.length > 0 && (
              <Alert variant="warning" dismissible={false}>
                <p className="text-sm">
                  These attributes have more than{" "}
                  {preview.data.maxValuesPerAttributeKey} values, too many to
                  set up in bulk. Map their values one at a time under Map by
                  attribute.
                </p>
                <ul className="mt-1 list-disc pl-4 text-sm">
                  {preview.data.hiddenAttributeKeys.map((hidden) => (
                    <li key={hidden.key}>
                      {hidden.key}: {pluralize(hidden.valueCount, "value")}
                    </li>
                  ))}
                </ul>
              </Alert>
            )}

            {creates.length > 0 && (
              <div className="space-y-1">
                <Text small className="font-medium">
                  Access for the {pluralize(creates.length, "new role")}
                </Text>
                <Select
                  value={grantSource}
                  onValueChange={setGrantSource}
                  disabled={apply.isPending}
                >
                  <SelectTrigger
                    className="w-full"
                    aria-label="Access for new roles"
                  >
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={NO_GRANTS}>
                      No access yet (add grants to each role later)
                    </SelectItem>
                    {roles
                      .filter((role) => role.grants.length > 0)
                      .map((role) => (
                        <SelectItem
                          key={role.principalUrn}
                          value={role.principalUrn}
                        >
                          Same access as {role.name} (
                          {pluralize(role.grants.length, "grant")})
                        </SelectItem>
                      ))}
                  </SelectContent>
                </Select>
                <Text small muted>
                  {grantRole
                    ? `Each new role gets a copy of ${grantRole.name}'s ${pluralize(grantRole.grants.length, "grant")}. Changing ${grantRole.name} later doesn't change them.`
                    : "New roles give their members nothing until you add grants to them."}
                </Text>
              </div>
            )}

            {reused.length > 0 && (
              <Text small muted>
                Reusing{" "}
                {reused
                  .map(
                    (role) =>
                      `${role.name} (${pluralize(role.grants.length, "grant")})`,
                  )
                  .join(", ")}{" "}
                gives the matching directory people that role&apos;s access as
                it is now.
              </Text>
            )}

            {tooMany && (
              <Alert variant="warning" dismissible={false}>
                Select at most {MAX_SETUP_ITEMS} sources at a time.
              </Alert>
            )}

            {apply.isError && !changed && (
              <Alert variant="error" dismissible={false}>
                {apply.error instanceof Error
                  ? apply.error.message
                  : "Couldn't apply the role mappings. Try again."}
              </Alert>
            )}
          </div>
        )}

        <Dialog.Footer>
          <Button
            variant="tertiary"
            onClick={onClose}
            disabled={apply.isPending}
          >
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            variant="primary"
            disabled={!canApply}
            onClick={() => {
              setChanged(false);
              apply.mutate({
                request: {
                  applyDirectoryRoleSetupForm: setupRequest(
                    rows,
                    grantRole?.grants,
                  ),
                },
              });
            }}
          >
            <Button.Text>
              {apply.isPending
                ? "Applying…"
                : `Apply to ${pluralize(selected.length, "source")}`}
            </Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

/** One source: whether to set it up, and which role it gets. */
function SetupRowItem({
  row,
  roles,
  roleByUrn,
  disabled,
  onChange,
}: {
  row: SetupRow;
  roles: Role[];
  roleByUrn: Map<string, Role>;
  disabled: boolean;
  onChange: (change: Partial<SetupRow>) => void;
}): JSX.Element {
  const reusedRole =
    row.choice.kind === "reuse" ? roleByUrn.get(row.choice.roleUrn) : undefined;
  // A new role's picker already says so; an existing role's grants are what
  // its new members get, so say how many.
  const detail =
    row.choice.kind === "create"
      ? undefined
      : reusedRole
        ? `Existing role, ${reusedRole.grants.length === 0 ? "no grants" : pluralize(reusedRole.grants.length, "grant")}`
        : "Existing role";
  return (
    <li className="grid grid-cols-[auto_1fr_16rem] items-center gap-3 px-3 py-2">
      <Checkbox
        checked={row.selected}
        disabled={disabled}
        onCheckedChange={(checked) => onChange({ selected: checked === true })}
        aria-label={`Set up ${row.item.sourceName}`}
      />
      <div className="min-w-0">
        <Text className="truncate font-medium">{row.item.sourceName}</Text>
        <Text muted small>
          {row.item.source.sourceKind === "group" ? "Group" : "Attribute value"}
          , {row.item.directoryPeople}{" "}
          {row.item.directoryPeople === 1
            ? "directory person"
            : "directory people"}
        </Text>
      </div>
      <div className="min-w-0 space-y-0.5">
        <Select
          value={choiceValue(row.choice)}
          onValueChange={(value) =>
            onChange({ choice: choiceFromValue(value), selected: true })
          }
          disabled={disabled}
        >
          <SelectTrigger
            className="w-full"
            aria-label={`Role for ${row.item.sourceName}`}
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {row.proposed.kind === "create" && (
              <SelectItem value={choiceValue(row.proposed)}>
                New role: {row.proposed.name}
              </SelectItem>
            )}
            {roles.map((role) => (
              <SelectItem
                key={role.principalUrn}
                value={`reuse:${role.principalUrn}`}
              >
                {role.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {detail && (
          <Text muted small className="truncate">
            {detail}
          </Text>
        )}
      </div>
    </li>
  );
}
