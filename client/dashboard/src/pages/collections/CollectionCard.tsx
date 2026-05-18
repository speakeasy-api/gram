import { DotCard } from "@/components/ui/dot-card";
import { Type } from "@/components/ui/type";
import { Badge } from "@/components/ui/badge";
import { Dialog } from "@/components/ui/dialog";
import { Combobox } from "@/components/ui/combobox";
import { ProjectAvatar } from "@/components/project-menu";
import { useOrganization } from "@/contexts/Auth";
import { AddServerDialog } from "@/pages/catalog/AddServerDialog";
import type { PulseMCPServer as CatalogServer } from "@/pages/catalog/hooks";
import { buildCollectionMcpJson } from "@/lib/mcp-json";
import { useOrgRoutes } from "@/routes";
import { Button, Stack } from "@speakeasy-api/moonshine";
import {
  ArrowRight,
  Download,
  Eye,
  FolderOpen,
  LayoutGrid,
  Lock,
  Server,
} from "lucide-react";
import { useMemo, useState } from "react";
import { Link, useNavigate } from "react-router";
import { toast } from "sonner";
import { useCollectionServers } from "./hooks";
import type { Collection } from "./types";

export function CollectionCard({ collection }: { collection: Collection }) {
  const orgRoutes = useOrgRoutes();
  const navigate = useNavigate();
  const organization = useOrganization();
  const defaultProjectSlug = organization.projects?.[0]?.slug;
  const { servers, rawServers, isLoading } = useCollectionServers(
    collection.slug,
  );
  const [selectedProjectSlug, setSelectedProjectSlug] = useState<
    string | undefined
  >(defaultProjectSlug);
  const [pendingInstall, setPendingInstall] = useState(false);
  const [bulkInstallServers, setBulkInstallServers] = useState<
    CatalogServer[] | null
  >(null);
  const [showAddDialog, setShowAddDialog] = useState(false);
  const serverCount = servers.length;
  const detailHref = orgRoutes.collections.detail.href(collection.slug ?? "");

  const installableServers: CatalogServer[] = useMemo(
    () =>
      rawServers.map((server) => ({
        ...server,
        meta: {},
      })),
    [rawServers],
  );
  const collectionMcpJson = useMemo(
    () => buildCollectionMcpJson(rawServers),
    [rawServers],
  );
  const installableServersWithEndpoint = useMemo(() => {
    const excludedSpecifiers = new Set(
      collectionMcpJson.excludedServers.map(
        (server) => server.registrySpecifier,
      ),
    );
    return installableServers.filter(
      (server) => !excludedSpecifiers.has(server.registrySpecifier),
    );
  }, [installableServers, collectionMcpJson.excludedServers]);

  const projects = useMemo(
    () => organization.projects ?? [],
    [organization.projects],
  );
  const projectOptions = useMemo(
    () =>
      projects.map((project) => ({
        ...project,
        value: project.slug,
        label: project.name,
        icon: (
          <ProjectAvatar
            project={project}
            className="h-4 min-h-4 w-4 min-w-4"
          />
        ),
      })),
    [projects],
  );
  const selectedProjectOption =
    projectOptions.find((project) => project.value === selectedProjectSlug) ??
    projectOptions[0];

  const handleInstallClick = (event: React.MouseEvent) => {
    event.stopPropagation();

    if (installableServersWithEndpoint.length === 0) return;

    if (collectionMcpJson.excludedCount > 0) {
      toast.info(
        `Installing ${installableServersWithEndpoint.length} of ${rawServers.length} servers (${collectionMcpJson.excludedCount} ${collectionMcpJson.excludedCount === 1 ? "has" : "have"} no active endpoint).`,
      );
    }

    setPendingInstall(true);
    setSelectedProjectSlug((current) => current ?? defaultProjectSlug);
  };

  return (
    <DotCard
      className="cursor-pointer"
      onClick={() => navigate(detailHref)}
      icon={
        <LayoutGrid className="text-muted-foreground h-10 w-10 opacity-60" />
      }
      overlay={
        collection.visibility === "private" ? (
          <div className="absolute top-3.5 left-3.5 z-10">
            <Badge
              variant="outline"
              className="border-muted-foreground/30 bg-background/80 text-muted-foreground backdrop-blur-sm"
            >
              <Lock className="mr-1 h-3 w-3" />
              Private
            </Badge>
          </div>
        ) : undefined
      }
    >
      <div className="mb-2 flex items-start justify-between gap-2">
        <div className="min-w-0 flex-1">
          <Type
            variant="subheading"
            as="div"
            className="text-md group-hover:text-primary truncate transition-colors"
            title={collection.name}
          >
            {collection.name}
          </Type>
        </div>
        <Badge variant="secondary">
          <Server className="mr-1 h-3 w-3" />
          {serverCount} {serverCount === 1 ? "server" : "servers"}
        </Badge>
      </div>

      {collection.description && (
        <Type small muted className="mb-3 line-clamp-2">
          {collection.description}
        </Type>
      )}

      <div className="mt-auto flex items-center justify-end gap-2 pt-2">
        {collection.visibility === "public" && (
          <Stack
            direction="horizontal"
            gap={1}
            align="center"
            className="text-muted-foreground mr-auto"
          >
            <Eye className="h-3.5 w-3.5" />
            <Type small muted>
              Public
            </Type>
          </Stack>
        )}

        <Link to={detailHref} onClick={(e) => e.stopPropagation()}>
          <Button variant="secondary" size="sm">
            <Button.Text>View</Button.Text>
            <Button.RightIcon>
              <ArrowRight className="h-4 w-4" />
            </Button.RightIcon>
          </Button>
        </Link>
        <Button
          variant="secondary"
          size="sm"
          disabled={
            isLoading ||
            installableServersWithEndpoint.length === 0 ||
            projects.length === 0
          }
          onClick={handleInstallClick}
        >
          <Button.Icon>
            <Download />
          </Button.Icon>
          <Button.Text>Install</Button.Text>
        </Button>
      </div>
      <Dialog
        open={pendingInstall}
        onOpenChange={(open) => {
          if (!open) {
            setPendingInstall(false);
          }
        }}
      >
        <Dialog.Content
          className="sm:max-w-md"
          onClick={(e) => e.stopPropagation()}
        >
          <Dialog.Header>
            <Dialog.Title>Select Project</Dialog.Title>
            <Dialog.Description>
              Choose where to install{" "}
              <span className="font-medium">
                {installableServersWithEndpoint.length} servers
              </span>
              .
            </Dialog.Description>
          </Dialog.Header>
          <div className="space-y-4 py-2">
            <div className="rounded-lg border p-3">
              <div className="flex items-center gap-2 text-sm font-medium">
                <Server className="h-4 w-4" />
                {installableServersWithEndpoint.length} servers from{" "}
                {collection.name}
              </div>
            </div>
            {projectOptions.length === 0 ? (
              <div className="flex flex-col items-center py-6 text-center">
                <FolderOpen className="text-muted-foreground mb-2 h-8 w-8" />
                <p className="text-muted-foreground text-sm">
                  No projects found.
                </p>
              </div>
            ) : (
              <div className="space-y-2">
                <label className="text-sm font-medium">Project</label>
                <Combobox
                  items={projectOptions}
                  selected={selectedProjectOption}
                  onSelectionChange={(project) =>
                    setSelectedProjectSlug(project.value)
                  }
                  className="w-full justify-between"
                >
                  {selectedProjectOption ? (
                    <div className="flex items-center gap-2">
                      <ProjectAvatar
                        project={selectedProjectOption}
                        className="h-4 min-h-4 w-4 min-w-4"
                      />
                      <span className="truncate">
                        {selectedProjectOption.label}
                      </span>
                    </div>
                  ) : (
                    <span>Select a project</span>
                  )}
                </Combobox>
              </div>
            )}
          </div>
          <Dialog.Footer>
            <Button
              variant="secondary"
              onClick={(e: React.MouseEvent) => {
                e.stopPropagation();
                setPendingInstall(false);
              }}
            >
              Cancel
            </Button>
            <Button
              disabled={!selectedProjectOption}
              onClick={(e: React.MouseEvent) => {
                e.stopPropagation();
                if (!selectedProjectOption) return;

                setSelectedProjectSlug(selectedProjectOption.value);
                setBulkInstallServers(installableServersWithEndpoint);
                setPendingInstall(false);
                setShowAddDialog(true);
              }}
            >
              Continue
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
      {bulkInstallServers && selectedProjectSlug && (
        <AddServerDialog
          servers={bulkInstallServers}
          projectSlug={selectedProjectSlug}
          open={showAddDialog}
          bulk
          onOpenChange={(open) => {
            setShowAddDialog(open);
            if (!open) {
              setTimeout(() => setBulkInstallServers(null), 300);
            }
          }}
        />
      )}
    </DotCard>
  );
}
