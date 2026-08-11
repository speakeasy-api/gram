import { ResourceListPage } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { Dialog } from "@/components/ui/Dialog";
import { DotRow } from "@/components/ui/DotRow";
import { DotTable } from "@/components/ui/DotTable";
import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import { useIsPlatformAdmin, useOrganization } from "@/contexts/Auth";
import { useOrgRoutes } from "@/routes";
import type { OrganizationRemoteSessionIssuer } from "@gram/client/models/components/organizationremotesessionissuer.js";
import { useDeleteOrganizationRemoteSessionIssuerMutation } from "@gram/client/react-query/deleteOrganizationRemoteSessionIssuer.js";
import { useListProjects } from "@gram/client/react-query/listProjects.js";
import { useMoveOrganizationRemoteSessionIssuerMutation } from "@gram/client/react-query/moveOrganizationRemoteSessionIssuer.js";
import { invalidateAllOrganizationRemoteSessionIssuer } from "@gram/client/react-query/organizationRemoteSessionIssuer.js";
import { useOrganizationRemoteSessionIssuerDeletePreflight } from "@gram/client/react-query/organizationRemoteSessionIssuerDeletePreflight.js";
import {
  invalidateAllOrganizationRemoteSessionIssuers,
  useOrganizationRemoteSessionIssuers,
} from "@gram/client/react-query/organizationRemoteSessionIssuers.js";
import { useRefreshOrganizationRemoteSessionIssuerMetadataMutation } from "@gram/client/react-query/refreshOrganizationRemoteSessionIssuerMetadata.js";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { Heading } from "@/components/ui/Heading";
import { Icon } from "@/components/ui/Icon";
import { Stack } from "@/components/ui/Stack";
import { useQueryClient } from "@tanstack/react-query";
import { MoreHorizontal, Plus } from "lucide-react";
import { useMemo, useState } from "react";
import { Outlet } from "react-router";
import { toast } from "sonner";
import { ConfirmDialog } from "./ConfirmDialog";
import { CreateRemoteIdentityProviderSheet } from "./CreateRemoteIdentityProviderSheet";
import { CreateRemoteSessionClientSheet } from "./CreateRemoteSessionClientSheet";
import { issuerDisplayName } from "./issuerDisplay";
import { MigrateIssuerDialog } from "./MigrateIssuerDialog";
import { migrationCandidates } from "./migrationCandidates";
import { remoteSessionScopeTier } from "@/lib/sources";

export function RemoteIdentityProvidersRoot(): JSX.Element {
  return <Outlet />;
}

export function RemoteIdentityProvidersPage(): JSX.Element {
  return (
    <RequireScope scope={["org:read", "org:admin"]} level="page">
      <RemoteIdentityProvidersOverview />
    </RequireScope>
  );
}

function RemoteIdentityProvidersOverview() {
  const queryClient = useQueryClient();
  const orgRoutes = useOrgRoutes();
  const isPlatformAdmin = useIsPlatformAdmin();
  const { data, isLoading } = useOrganizationRemoteSessionIssuers({});
  const [deleteTarget, setDeleteTarget] =
    useState<OrganizationRemoteSessionIssuer | null>(null);
  const [moveTarget, setMoveTarget] =
    useState<OrganizationRemoteSessionIssuer | null>(null);
  const [migrateSource, setMigrateSource] =
    useState<OrganizationRemoteSessionIssuer | null>(null);
  const [addClientTarget, setAddClientTarget] =
    useState<OrganizationRemoteSessionIssuer | null>(null);
  const [createOpen, setCreateOpen] = useState(false);

  const allItems = useMemo(() => data?.result.items ?? [], [data]);

  // Three tenancy tiers. Platform issuers are inherited from the shared catalog
  // and are read-only to the tenant, so they render in their own section without
  // the move/consolidate/delete actions that would 404 against a global row.
  const { platform, organizational, projectSpecific } = useMemo(
    () => ({
      platform: allItems.filter(
        (item) => remoteSessionScopeTier(item.issuer) === "platform",
      ),
      organizational: allItems.filter(
        (item) => remoteSessionScopeTier(item.issuer) === "organization",
      ),
      projectSpecific: allItems.filter(
        (item) => remoteSessionScopeTier(item.issuer) === "project",
      ),
    }),
    [allItems],
  );

  // Promoting a project-specific issuer to organizational applies immediately
  // from the menu (no project to pick); the picker dialog handles the cases that
  // need a target project.
  const makeOrganizational = useMoveOrganizationRemoteSessionIssuerMutation({
    onSuccess: async () => {
      await invalidateAllOrganizationRemoteSessionIssuers(queryClient, {
        refetchType: "all",
      });
      toast.success("Provider is now organizational");
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to move provider",
      );
    },
  });

  const handleMakeOrganizational = (item: OrganizationRemoteSessionIssuer) => {
    makeOrganizational.mutate({
      request: { moveIssuerRequestBody: { id: item.issuer.id } },
    });
  };

  // One endpoint serves both tables: the org-scoped refresh resolves
  // organizational and project-specific issuers alike.
  const refreshMetadata =
    useRefreshOrganizationRemoteSessionIssuerMetadataMutation({
      onSuccess: async (result) => {
        await Promise.all([
          invalidateAllOrganizationRemoteSessionIssuers(queryClient, {
            refetchType: "all",
          }),
          // The detail page reads the singular query, which the list
          // invalidation above does not cover. Without this, opening the
          // provider right after refreshing it shows the pre-refresh endpoints.
          invalidateAllOrganizationRemoteSessionIssuer(queryClient, {
            refetchType: "all",
          }),
        ]);
        const label = issuerDisplayName(result.issuer);
        if (result.discoveryWarnings.length > 0) {
          // The refresh did persist — these describe RFC 8414 deviations worth
          // an operator's attention, not a failure. Anything severe enough to
          // distrust the document fails the request instead.
          toast.warning(`Refreshed ${label} with warnings`, {
            description: result.discoveryWarnings.join(" "),
          });
          return;
        }
        toast.success(`Refreshed discoverable metadata for ${label}`);
      },
      onError: (error) => {
        toast.error(
          error instanceof Error ? error.message : "Failed to refresh metadata",
        );
      },
    });

  const handleRefreshMetadata = (item: OrganizationRemoteSessionIssuer) => {
    refreshMetadata.mutate({
      request: { riskIDRequestBody: { id: item.issuer.id } },
    });
  };

  return (
    <>
      <ResourceListPage
        title="Organizational Remote Identity Providers"
        description="Identity providers shared across every project in the organization. Prefer creating clients on platform maintained providers when available unless client setup documentation needs customization for your organization workflows."
        primaryAction={
          <RequireScope scope="org:admin" level="component">
            <Button size="sm" onClick={() => setCreateOpen(true)}>
              <Button.LeftIcon>
                <Plus />
              </Button.LeftIcon>
              <Button.Text>New Remote Identity Provider</Button.Text>
            </Button>
          </RequireScope>
        }
      >
        <IssuerTable
          items={organizational}
          isLoading={isLoading}
          showProject={false}
          emptyMessage="No organizational identity providers yet."
          onDelete={setDeleteTarget}
          onMakeOrganizational={handleMakeOrganizational}
          onMoveToProject={setMoveTarget}
          onConsolidate={setMigrateSource}
          onRefreshMetadata={handleRefreshMetadata}
          refreshPending={refreshMetadata.isPending}
        />

        {/* The page header (eyebrow + display title) is rendered once by the
            template above; the remaining tiers are plain section headings. */}
        <Stack gap={6} className="mt-3 mb-6">
          <div>
            <Heading variant="h4" className="mb-2">
              Project-Specific Remote Identity Providers
            </Heading>
            <Text muted small className="max-w-2xl">
              Identity providers within a single project in the organization.
              Prefer creating clients on platform maintained providers when
              available unless client setup documentation needs customization
              for your organization workflows.
            </Text>
          </div>
          <IssuerTable
            items={projectSpecific}
            isLoading={isLoading}
            showProject
            emptyMessage="No project-specific identity providers yet."
            onDelete={setDeleteTarget}
            onMakeOrganizational={handleMakeOrganizational}
            onMoveToProject={setMoveTarget}
            onConsolidate={setMigrateSource}
            onRefreshMetadata={handleRefreshMetadata}
            refreshPending={refreshMetadata.isPending}
          />
        </Stack>

        {platform.length > 0 && (
          <Stack gap={6} className="mt-3 mb-6">
            <Stack
              direction="horizontal"
              justify="space-between"
              align="center"
              gap={4}
            >
              <div className="min-w-0">
                <Heading variant="h4" className="mb-2">
                  Platform Remote Identity Providers
                </Heading>
                <Text muted small className="max-w-2xl">
                  Common identity providers maintained by the platform
                  administrators for configuring your own clients. Prefer using
                  these over creating duplicate providers unless the client
                  setup documentation needs to be customized when creating MCP
                  Servers.
                </Text>
              </div>
              {/* Platform admins curate these on their own page. The CTA is the
                  only platform-admin-aware chrome on this tenant surface, and it
                  is a link — it grants nothing that the catalog page does not
                  gate again on its own. */}
              {isPlatformAdmin ? (
                <Button
                  size="sm"
                  variant="secondary"
                  className="shrink-0"
                  onClick={() =>
                    orgRoutes.platformRemoteIdentityProviders.goTo()
                  }
                >
                  <Button.Text>Manage Platform Providers</Button.Text>
                </Button>
              ) : null}
            </Stack>
            <IssuerTable
              items={platform}
              isLoading={isLoading}
              showProject={false}
              readOnly
              onAddClient={setAddClientTarget}
              emptyMessage="No platform identity providers available."
              onDelete={setDeleteTarget}
              onMakeOrganizational={handleMakeOrganizational}
              onMoveToProject={setMoveTarget}
              onConsolidate={setMigrateSource}
              onRefreshMetadata={handleRefreshMetadata}
              refreshPending={refreshMetadata.isPending}
            />
          </Stack>
        )}
      </ResourceListPage>

      <CreateRemoteIdentityProviderSheet
        open={createOpen}
        onOpenChange={setCreateOpen}
      />

      {deleteTarget && (
        <DeleteIssuerDialog
          issuerId={deleteTarget.issuer.id}
          issuerLabel={issuerDisplayName(deleteTarget.issuer)}
          knownClientCount={deleteTarget.clientCount}
          onClose={() => setDeleteTarget(null)}
        />
      )}

      {moveTarget && (
        <MoveToProjectDialog
          issuer={moveTarget.issuer}
          onClose={() => setMoveTarget(null)}
        />
      )}

      {migrateSource && (
        <MigrateIssuerDialog
          source={migrateSource}
          candidates={migrationCandidates(migrateSource, allItems)}
          onClose={() => setMigrateSource(null)}
        />
      )}

      {/* Keyed on the issuer so reopening the sheet for a different provider
          remounts it rather than reusing the previous provider's draft. */}
      {addClientTarget && (
        <CreateRemoteSessionClientSheet
          key={addClientTarget.issuer.id}
          open
          onOpenChange={(open) => {
            if (!open) setAddClientTarget(null);
          }}
          issuer={addClientTarget.issuer}
        />
      )}
    </>
  );
}

function IssuerTable({
  items,
  isLoading,
  showProject,
  readOnly = false,
  onAddClient,
  emptyMessage,
  onDelete,
  onMakeOrganizational,
  onMoveToProject,
  onConsolidate,
  onRefreshMetadata,
  refreshPending,
}: {
  items: OrganizationRemoteSessionIssuer[];
  isLoading: boolean;
  showProject: boolean;
  // readOnly drops the issuer-mutation actions for tiers the tenant cannot
  // change (platform issuers): the row still links to the detail view.
  readOnly?: boolean;
  // Registering a client is not an issuer mutation — it creates a tenant-owned
  // row that happens to point at this issuer — so read-only tiers still offer
  // it. Supplying this puts the actions column back with just that one action.
  onAddClient?: (item: OrganizationRemoteSessionIssuer) => void;
  emptyMessage: string;
  onDelete: (item: OrganizationRemoteSessionIssuer) => void;
  onMakeOrganizational: (item: OrganizationRemoteSessionIssuer) => void;
  onMoveToProject: (item: OrganizationRemoteSessionIssuer) => void;
  onConsolidate: (item: OrganizationRemoteSessionIssuer) => void;
  onRefreshMetadata: (item: OrganizationRemoteSessionIssuer) => void;
  refreshPending: boolean;
}) {
  const orgRoutes = useOrgRoutes();

  const showActions = !readOnly || !!onAddClient;
  const actionsHeader = showActions ? [{ label: "" }] : [];
  const headers = showProject
    ? [
        { label: "Provider" },
        { label: "Project" },
        { label: "Clients" },
        ...actionsHeader,
      ]
    : [{ label: "Provider" }, { label: "Clients" }, ...actionsHeader];

  if (!isLoading && items.length === 0) {
    return (
      <Stack
        className="border-border border py-8"
        align="center"
        justify="center"
      >
        <Text variant="body" muted>
          {emptyMessage}
        </Text>
      </Stack>
    );
  }

  return (
    <DotTable headers={headers}>
      {items.map((item) => (
        <DotRow
          key={item.issuer.id}
          icon={
            <Icon
              name="fingerprint"
              className="text-muted-foreground h-5 w-5"
            />
          }
          href={orgRoutes.remoteIdentityProviders.issuerDetail.href(
            item.issuer.id,
          )}
          ariaLabel={`View remote identity provider ${issuerDisplayName(item.issuer)}`}
        >
          <td className="px-3 py-3">
            <Text
              variant="subheading"
              as="div"
              className="group-hover:text-primary truncate text-sm transition-colors group-hover:underline"
            >
              {issuerDisplayName(item.issuer)}
            </Text>
            <Text small muted as="div" className="truncate">
              {item.issuer.issuer}
            </Text>
          </td>
          {showProject && (
            <td className="px-3 py-3">
              <Text small muted>
                {item.projectName || "—"}
              </Text>
            </td>
          )}
          <td className="px-3 py-3">
            <Text small muted>
              {item.clientCount} {item.clientCount === 1 ? "client" : "clients"}
            </Text>
          </td>
          {showActions && (
            <td className="px-3 py-3 text-right">
              {readOnly ? (
                <InheritedIssuerRowActions
                  issuerLabel={issuerDisplayName(item.issuer)}
                  onAddClient={() => onAddClient?.(item)}
                />
              ) : (
                <RowActions
                  item={item}
                  onDelete={() => onDelete(item)}
                  onMakeOrganizational={() => onMakeOrganizational(item)}
                  onMoveToProject={() => onMoveToProject(item)}
                  onConsolidate={() => onConsolidate(item)}
                  onRefreshMetadata={() => onRefreshMetadata(item)}
                  refreshPending={refreshPending}
                />
              )}
            </td>
          )}
        </DotRow>
      ))}
    </DotTable>
  );
}

// InheritedIssuerRowActions is the menu for issuers the tenant cannot modify
// (the platform tier). Registering a client is the one thing they can do with
// one, so it is the only entry. Gated on org:admin to match the New Client
// control on the issuer's Clients tab, which calls the same endpoint.
function InheritedIssuerRowActions({
  issuerLabel,
  onAddClient,
}: {
  issuerLabel: string;
  onAddClient: () => void;
}) {
  return (
    <div className="relative z-20" onClick={(e) => e.stopPropagation()}>
      <RequireScope scope="org:admin" level="section">
        {/* Non-modal for the same reason as RowActions below: creating a client
            invalidates the issuers query and reorders rows, unmounting this
            menu mid-close, which would strand Radix's body pointer-events
            lock. */}
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger asChild>
            <Button
              variant="tertiary"
              size="sm"
              aria-label={`Actions for ${issuerLabel}`}
            >
              <Button.LeftIcon>
                <MoreHorizontal className="h-4 w-4" />
              </Button.LeftIcon>
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onClick={onAddClient}>
              Add Client
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </RequireScope>
    </div>
  );
}

function RowActions({
  item,
  onDelete,
  onMakeOrganizational,
  onMoveToProject,
  onConsolidate,
  onRefreshMetadata,
  refreshPending,
}: {
  item: OrganizationRemoteSessionIssuer;
  onDelete: () => void;
  onMakeOrganizational: () => void;
  onMoveToProject: () => void;
  onConsolidate: () => void;
  onRefreshMetadata: () => void;
  refreshPending: boolean;
}) {
  const isOrganizational = !item.issuer.projectId;

  return (
    <div className="relative z-20" onClick={(e) => e.stopPropagation()}>
      {/*
        Non-modal so Radix never locks `pointer-events: none` on <body>. The move
        actions mutate and invalidate the issuers query, which reorders the rows
        across the two tables and unmounts this menu mid-close; a modal menu would
        then leave the body lock stuck, making the page unclickable until refresh.
      */}
      <RequireScope scope="org:admin" level="section">
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger asChild>
            <Button variant="tertiary" size="sm">
              <Button.LeftIcon>
                <MoreHorizontal className="h-4 w-4" />
              </Button.LeftIcon>
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem
              onClick={onRefreshMetadata}
              disabled={refreshPending}
            >
              Refresh Discoverable Metadata
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            {isOrganizational ? (
              <DropdownMenuItem onClick={onMoveToProject}>
                Make project-specific
              </DropdownMenuItem>
            ) : (
              <>
                <DropdownMenuItem onClick={onMakeOrganizational}>
                  Make organizational
                </DropdownMenuItem>
                <DropdownMenuItem onClick={onMoveToProject}>
                  Move to another project
                </DropdownMenuItem>
              </>
            )}
            <DropdownMenuItem onClick={onConsolidate}>
              Consolidate into another provider
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onClick={onDelete}>Delete</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </RequireScope>
    </div>
  );
}

// Sentinel for the "no project" (organizational) selection. Radix Select treats
// the empty string specially, so we use an explicit value and map it back to an
// omitted projectId on submit. Mirrors CreateRemoteIdentityProviderSheet.
const ORGANIZATIONAL = "organizational";

// MoveToProjectDialog re-scopes an issuer to a chosen project (or back to
// organizational). It's used for org→project and project→project moves; the
// immediate project→organizational path lives on the row menu. The picker
// preselects the issuer's current scope so a project-specific issuer opens on its
// owning project.
function MoveToProjectDialog({
  issuer,
  onClose,
}: {
  issuer: OrganizationRemoteSessionIssuer["issuer"];
  onClose: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const organization = useOrganization();
  const { data: projectsData } = useListProjects({
    organizationId: organization.id,
  });
  const projects = useMemo(() => projectsData?.projects ?? [], [projectsData]);

  const [projectId, setProjectId] = useState<string>(
    issuer.projectId || ORGANIZATIONAL,
  );

  const move = useMoveOrganizationRemoteSessionIssuerMutation({
    onSuccess: async () => {
      await invalidateAllOrganizationRemoteSessionIssuers(queryClient, {
        refetchType: "all",
      });
      toast.success("Provider moved");
      onClose();
    },
    onError: (error) => {
      console.error("Move remote identity provider failed", error);
    },
  });

  const moveError = move.error
    ? move.error instanceof Error && move.error.message
      ? move.error.message
      : "An unexpected error occurred. Please try again."
    : null;

  // No-op moves (target equals current scope) are disabled so the action always
  // changes something.
  const unchanged =
    projectId === (issuer.projectId || ORGANIZATIONAL) || move.isPending;

  const handleMove = () => {
    move.mutate({
      request: {
        moveIssuerRequestBody: {
          id: issuer.id,
          projectId: projectId === ORGANIZATIONAL ? undefined : projectId,
        },
      },
    });
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Move identity provider</Dialog.Title>
          <Dialog.Description>
            Choose a project to scope this provider to, or make it
            organizational (inherited by every project).
          </Dialog.Description>
        </Dialog.Header>

        <Stack gap={2}>
          <Label className="text-muted-foreground text-xs">Scope</Label>
          <Select value={projectId} onValueChange={setProjectId}>
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ORGANIZATIONAL}>
                Organizational (all projects)
              </SelectItem>
              {projects.map((project) => (
                <SelectItem key={project.id} value={project.id}>
                  {project.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Stack>

        {moveError && (
          <Alert variant="error" dismissible={false}>
            {moveError}
          </Alert>
        )}

        <Dialog.Footer>
          <Button
            variant="tertiary"
            onClick={onClose}
            disabled={move.isPending}
          >
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button variant="primary" onClick={handleMove} disabled={unchanged}>
            <Button.Text>{move.isPending ? "Moving…" : "Move"}</Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}

export function DeleteIssuerDialog({
  issuerId,
  issuerLabel,
  knownClientCount,
  onClose,
  onDeleted,
}: {
  issuerId: string;
  issuerLabel: string;
  knownClientCount?: number;
  onClose: () => void;
  onDeleted?: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const { data: preflight, isLoading: preflightLoading } =
    useOrganizationRemoteSessionIssuerDeletePreflight({ id: issuerId });

  const deleteMutation = useDeleteOrganizationRemoteSessionIssuerMutation({
    onSuccess: async () => {
      await invalidateAllOrganizationRemoteSessionIssuers(queryClient, {
        refetchType: "all",
      });
      toast.success("Remote identity provider deleted");
      onDeleted?.();
      onClose();
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to delete identity provider",
      );
    },
  });

  const clientCount = preflight?.clientCount ?? knownClientCount ?? 0;

  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title={`Delete "${issuerLabel}"?`}
      description="This permanently removes the remote identity provider. Clients must be deleted first."
      confirmLabel="Delete provider"
      isPending={deleteMutation.isPending}
      impact={{
        summary: `${clientCount} ${clientCount === 1 ? "client is" : "clients are"} registered with this provider.`,
        mcpServerNames: preflight?.mcpServerNames,
        isLoading: preflightLoading,
      }}
      onConfirm={() => deleteMutation.mutate({ request: { id: issuerId } })}
    />
  );
}
