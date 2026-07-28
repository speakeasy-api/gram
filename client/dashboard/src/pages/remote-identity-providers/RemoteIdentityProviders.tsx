import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Dialog } from "@/components/ui/dialog";
import { DotRow } from "@/components/ui/dot-row";
import { DotTable } from "@/components/ui/dot-table";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Type } from "@/components/ui/type";
import { useOrganization } from "@/contexts/Auth";
import { useOrgRoutes } from "@/routes";
import type { OrganizationRemoteSessionIssuer } from "@gram/client/models/components/organizationremotesessionissuer.js";
import { useDeleteOrganizationRemoteSessionIssuerMutation } from "@gram/client/react-query/deleteOrganizationRemoteSessionIssuer.js";
import { useListProjects } from "@gram/client/react-query/listProjects.js";
import { useMoveOrganizationRemoteSessionIssuerMutation } from "@gram/client/react-query/moveOrganizationRemoteSessionIssuer.js";
import { useOrganizationRemoteSessionIssuerDeletePreflight } from "@gram/client/react-query/organizationRemoteSessionIssuerDeletePreflight.js";
import {
  invalidateAllOrganizationRemoteSessionIssuers,
  useOrganizationRemoteSessionIssuers,
} from "@gram/client/react-query/organizationRemoteSessionIssuers.js";
import {
  Alert,
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  Icon,
  Stack,
} from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { MoreHorizontal, Plus } from "lucide-react";
import { useMemo, useState } from "react";
import { Outlet } from "react-router";
import { toast } from "sonner";
import { ConfirmDialog } from "./ConfirmDialog";
import { CreateRemoteIdentityProviderSheet } from "./CreateRemoteIdentityProviderSheet";
import { issuerDisplayName } from "./issuerDisplay";
import { MigrateIssuerDialog } from "./MigrateIssuerDialog";
import { migrationCandidates } from "./migrationCandidates";

export function RemoteIdentityProvidersRoot(): JSX.Element {
  return <Outlet />;
}

export function RemoteIdentityProvidersPage(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["org:read", "org:admin"]} level="page">
          <RemoteIdentityProvidersOverview />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

function RemoteIdentityProvidersOverview() {
  const queryClient = useQueryClient();
  const { data, isLoading } = useOrganizationRemoteSessionIssuers({});
  const [deleteTarget, setDeleteTarget] =
    useState<OrganizationRemoteSessionIssuer | null>(null);
  const [moveTarget, setMoveTarget] =
    useState<OrganizationRemoteSessionIssuer | null>(null);
  const [migrateSource, setMigrateSource] =
    useState<OrganizationRemoteSessionIssuer | null>(null);
  const [createOpen, setCreateOpen] = useState(false);

  const allItems = useMemo(() => data?.result.items ?? [], [data]);

  const { organizational, projectSpecific } = useMemo(
    () => ({
      organizational: allItems.filter((item) => !item.issuer.projectId),
      projectSpecific: allItems.filter((item) => !!item.issuer.projectId),
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

  return (
    <>
      <Page.Section>
        <Page.Section.Title>
          Organizational Remote Identity Providers
        </Page.Section.Title>
        <Page.Section.CTA>
          <RequireScope scope="org:admin" level="component">
            <Button size="sm" onClick={() => setCreateOpen(true)}>
              <Button.LeftIcon>
                <Plus />
              </Button.LeftIcon>
              <Button.Text>New Remote Identity Provider</Button.Text>
            </Button>
          </RequireScope>
        </Page.Section.CTA>
        <Page.Section.Description className="max-w-2xl">
          Upstream identity providers shared across every project in the
          organization. These have no owning project and are inherited
          everywhere.
        </Page.Section.Description>
        <Page.Section.Body>
          <IssuerTable
            items={organizational}
            isLoading={isLoading}
            showProject={false}
            emptyMessage="No organizational identity providers yet."
            onDelete={setDeleteTarget}
            onMakeOrganizational={handleMakeOrganizational}
            onMoveToProject={setMoveTarget}
            onConsolidate={setMigrateSource}
          />
        </Page.Section.Body>
      </Page.Section>

      <Page.Section>
        <Page.Section.Title>
          Project-Specific Remote Identity Providers
        </Page.Section.Title>
        <Page.Section.Description className="max-w-2xl">
          Upstream identity providers scoped to a single project.
        </Page.Section.Description>
        <Page.Section.Body>
          <IssuerTable
            items={projectSpecific}
            isLoading={isLoading}
            showProject
            emptyMessage="No project-specific identity providers yet."
            onDelete={setDeleteTarget}
            onMakeOrganizational={handleMakeOrganizational}
            onMoveToProject={setMoveTarget}
            onConsolidate={setMigrateSource}
          />
        </Page.Section.Body>
      </Page.Section>

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
    </>
  );
}

function IssuerTable({
  items,
  isLoading,
  showProject,
  emptyMessage,
  onDelete,
  onMakeOrganizational,
  onMoveToProject,
  onConsolidate,
}: {
  items: OrganizationRemoteSessionIssuer[];
  isLoading: boolean;
  showProject: boolean;
  emptyMessage: string;
  onDelete: (item: OrganizationRemoteSessionIssuer) => void;
  onMakeOrganizational: (item: OrganizationRemoteSessionIssuer) => void;
  onMoveToProject: (item: OrganizationRemoteSessionIssuer) => void;
  onConsolidate: (item: OrganizationRemoteSessionIssuer) => void;
}) {
  const orgRoutes = useOrgRoutes();

  const headers = showProject
    ? [
        { label: "Provider" },
        { label: "Project" },
        { label: "Clients" },
        { label: "" },
      ]
    : [{ label: "Provider" }, { label: "Clients" }, { label: "" }];

  if (!isLoading && items.length === 0) {
    return (
      <Type muted className="py-8 text-center">
        {emptyMessage}
      </Type>
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
            <Type
              variant="subheading"
              as="div"
              className="group-hover:text-primary truncate text-sm transition-colors group-hover:underline"
            >
              {issuerDisplayName(item.issuer)}
            </Type>
            <Type small muted as="div" className="truncate">
              {item.issuer.issuer}
            </Type>
          </td>
          {showProject && (
            <td className="px-3 py-3">
              <Type small muted>
                {item.projectName || "—"}
              </Type>
            </td>
          )}
          <td className="px-3 py-3">
            <Type small muted>
              {item.clientCount} {item.clientCount === 1 ? "client" : "clients"}
            </Type>
          </td>
          <td className="px-3 py-3 text-right">
            <RowActions
              item={item}
              onDelete={() => onDelete(item)}
              onMakeOrganizational={() => onMakeOrganizational(item)}
              onMoveToProject={() => onMoveToProject(item)}
              onConsolidate={() => onConsolidate(item)}
            />
          </td>
        </DotRow>
      ))}
    </DotTable>
  );
}

function RowActions({
  item,
  onDelete,
  onMakeOrganizational,
  onMoveToProject,
  onConsolidate,
}: {
  item: OrganizationRemoteSessionIssuer;
  onDelete: () => void;
  onMakeOrganizational: () => void;
  onMoveToProject: () => void;
  onConsolidate: () => void;
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
