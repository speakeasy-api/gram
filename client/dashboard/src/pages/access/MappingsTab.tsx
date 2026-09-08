import { Badge } from "@/components/ui/Badge";
import { Heading } from "@/components/ui/Heading";
import { Text } from "@/components/ui/Text";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Button } from "@/components/ui/Button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { Icon } from "@/components/ui/Icon";
import { RequireScope } from "@/components/require-scope";
import { TableRowContextMenu } from "@/components/table-row-context-menu";
import type { Action } from "@/components/ui/MoreActions";
import { useRBAC } from "@/hooks/useRBAC";
import { cn } from "@/lib/utils";
import { useOrganization } from "@/contexts/Auth";
import { useOrgRoutes } from "@/routes";
import { ConfirmDialog } from "@/pages/remote-identity-providers/ConfirmDialog";
import type { DirectoryMapping } from "@gram/client/models/components/directorymapping.js";
import {
  invalidateAllDirectoryMappings,
  useDirectoryMappings,
} from "@gram/client/react-query/directoryMappings.js";
import { useDeleteDirectoryMappingMutation } from "@gram/client/react-query/deleteDirectoryMapping.js";
import { useQueryClient } from "@tanstack/react-query";
import { Ellipsis } from "lucide-react";
import { useMemo, useState } from "react";
import { Link } from "react-router";
import { MappingDialog } from "./MappingDialog";
import {
  mappingKindLabel,
  mappingLabel,
  unusedMappingTargets,
} from "./mappingHelpers";
import { visiblePermissionCount } from "./roleDialogState";

function mappingActions({
  onEdit,
  onDelete,
}: {
  onEdit: () => void;
  onDelete: () => void;
}): Action[] {
  return [
    { label: "Edit", onClick: onEdit },
    { label: "Delete", destructive: true, onClick: onDelete },
  ];
}

function MappingActionsMenu({
  onEdit,
  onDelete,
}: {
  onEdit: () => void;
  onDelete: () => void;
}) {
  const [open, setOpen] = useState(false);
  const actions = mappingActions({ onEdit, onDelete });

  return (
    <RequireScope scope="org:admin" level="component">
      {({ disabled }) => (
        <DropdownMenu open={open} onOpenChange={setOpen} modal={false}>
          <DropdownMenuTrigger asChild disabled={disabled}>
            <button
              type="button"
              disabled={disabled}
              className={cn(
                "text-muted-foreground hover:bg-accent hover:text-foreground flex h-8 w-8 cursor-pointer items-center justify-center transition-colors",
                open && "bg-accent text-foreground",
                disabled && "cursor-not-allowed",
              )}
            >
              <Ellipsis className="h-4 w-4" />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            {actions.map((action) => (
              <DropdownMenuItem
                key={action.label}
                onSelect={() => {
                  void setTimeout(action.onClick, 0);
                }}
              >
                {action.label}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
    </RequireScope>
  );
}

function MappingRow({
  mapping,
  canManage,
  onEdit,
  onDelete,
}: {
  mapping: DirectoryMapping;
  canManage: boolean;
  onEdit: () => void;
  onDelete: () => void;
}): JSX.Element {
  const actions: Action[] = canManage
    ? mappingActions({ onEdit, onDelete })
    : [];

  return (
    <TableRowContextMenu actions={actions}>
      <div
        role={canManage ? "button" : undefined}
        tabIndex={canManage ? 0 : undefined}
        onClick={canManage ? onEdit : undefined}
        onKeyDown={
          canManage
            ? (e) => {
                if (e.key === "Enter" || e.key === " ") {
                  e.preventDefault();
                  onEdit();
                }
              }
            : undefined
        }
        className={cn(
          "border-border col-span-full grid grid-cols-subgrid items-center gap-x-6 border-b px-4 py-3 last:border-b-0",
          canManage && "hover:bg-muted/50 cursor-pointer",
        )}
      >
        <div className="flex min-w-0 items-center gap-2">
          <Text variant="body" className="truncate font-medium">
            {mappingLabel(mapping)}
          </Text>
          <Badge variant="neutral" size="sm">
            {mappingKindLabel(mapping.kind)}
          </Badge>
        </div>
        <Text variant="body">{visiblePermissionCount(mapping.grants)}</Text>
        <Text variant="body" className="text-muted-foreground">
          {mapping.memberCount}
        </Text>
        <div aria-hidden />
        <div onClick={(e) => e.stopPropagation()} className="flex justify-end">
          <MappingActionsMenu onEdit={onEdit} onDelete={onDelete} />
        </div>
      </div>
    </TableRowContextMenu>
  );
}

export function MappingsTab(): JSX.Element {
  const { hasAnyScope } = useRBAC();
  const canManage = hasAnyScope(["org:admin"]);
  const organization = useOrganization();
  const orgRoutes = useOrgRoutes();
  const queryClient = useQueryClient();
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [editingMapping, setEditingMapping] = useState<DirectoryMapping | null>(
    null,
  );
  const [deletingMapping, setDeletingMapping] =
    useState<DirectoryMapping | null>(null);
  const { data, isLoading } = useDirectoryMappings();
  const mappings = data?.mappings ?? [];
  const groups = data?.groups ?? [];
  const attributes = data?.attributes ?? [];
  const availableTargets = useMemo(
    () =>
      unusedMappingTargets(
        data?.groups ?? [],
        data?.attributes ?? [],
        (data?.mappings ?? []).map((mapping) => mapping.principalUrn),
      ),
    [data],
  );
  const hasCatalog = groups.length > 0 || attributes.length > 0;
  const deleteMapping = useDeleteDirectoryMappingMutation({
    onSuccess: async () => {
      await invalidateAllDirectoryMappings(queryClient);
    },
  });

  let body: JSX.Element;
  if (isLoading) {
    body = (
      <div className="mt-4">
        <SkeletonTable />
      </div>
    );
  } else if (!hasCatalog) {
    body = (
      <div className="mt-4">
        <InlineEmptyState
          icon="users"
          heading="No directory groups or attributes yet"
          description="Connect Directory Sync under Identity to map permissions to groups and identity-provider attributes."
          action={
            <Button variant="secondary" asChild>
              <Link to={orgRoutes.identity.href()}>Open identity settings</Link>
            </Button>
          }
        />
      </div>
    );
  } else {
    body = (
      <div className="border-border mt-4 grid grid-cols-[minmax(0,1fr)_max-content_max-content_1fr_max-content] overflow-hidden border">
        <div className="border-border col-span-full grid grid-cols-subgrid items-center gap-x-6 border-b px-4 py-2.5">
          <div className="text-eyebrow">Target</div>
          <div className="text-eyebrow">Permissions</div>
          <div className="text-eyebrow">Members</div>
          <div aria-hidden />
          <div className="sr-only">Actions</div>
        </div>
        {mappings.length === 0 ? (
          <div className="text-muted-foreground col-span-full p-4">
            No directory mappings have been created yet.
          </div>
        ) : (
          mappings.map((mapping) => (
            <MappingRow
              key={mapping.principalUrn}
              mapping={mapping}
              canManage={canManage}
              onEdit={() => setEditingMapping(mapping)}
              onDelete={() => setDeletingMapping(mapping)}
            />
          ))
        )}
      </div>
    );
  }

  return (
    <div>
      <div className="mb-1 flex items-center justify-between">
        <div>
          <Heading variant="h4">Directory mappings</Heading>
          <Text muted small className="mt-1">
            Grant extra permissions to identity-provider groups and attributes.
            These mappings are Gram-local and do not replace SCIM role
            assignment.
          </Text>
        </div>
        <RequireScope scope="org:admin" level="component">
          <Button
            onClick={() => setIsCreateOpen(true)}
            disabled={!hasCatalog || availableTargets.length === 0}
          >
            <Button.LeftIcon>
              <Icon name="plus" className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Add Mapping</Button.Text>
          </Button>
        </RequireScope>
      </div>

      {body}

      {organization.scimEnabled ? (
        <Text muted small className="mt-4">
          SCIM still assigns roles from your identity provider. Mappings add
          grants on top of those roles.
        </Text>
      ) : null}

      <MappingDialog
        open={isCreateOpen || !!editingMapping}
        onOpenChange={(open) => {
          if (!open) {
            setIsCreateOpen(false);
            setEditingMapping(null);
          }
        }}
        editingMapping={editingMapping}
        availableTargets={availableTargets}
      />

      <ConfirmDialog
        open={!!deletingMapping}
        onOpenChange={(open) => {
          if (!open) setDeletingMapping(null);
        }}
        title="Delete mapping"
        description={
          deletingMapping ? (
            <>
              Remove all permission grants assigned to{" "}
              <code className="bg-muted px-1 py-0.5 font-mono font-bold">
                {mappingLabel(deletingMapping)}
              </code>
              ? Matching members keep their roles.
            </>
          ) : (
            ""
          )
        }
        confirmLabel="Delete mapping"
        isPending={deleteMapping.isPending}
        onConfirm={() => {
          if (!deletingMapping) return;
          void deleteMapping
            .mutateAsync({
              request: { principalUrn: deletingMapping.principalUrn },
            })
            .then(() => setDeletingMapping(null));
        }}
      />
    </div>
  );
}
