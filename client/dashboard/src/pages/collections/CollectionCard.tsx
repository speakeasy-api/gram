import { Card } from "@/components/ui/Card";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { Text } from "@/components/ui/Text";
import { Badge } from "@/components/ui/Badge";
import { useOrganization } from "@/contexts/Auth";
import type { PulseMCPServer as CatalogServer } from "@/pages/catalog/hooks";
import { toolStats } from "@/pages/catalog/hooks/serverMetadata";
import { buildCollectionMcpJson } from "@/lib/mcp-json";
import { useOrgRoutes } from "@/routes";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import {
  ArrowRight,
  Download,
  Eye,
  LayoutGrid,
  Lock,
  Server,
} from "lucide-react";
import { useMemo, useState } from "react";
import { Link, useNavigate } from "react-router";
import { toast } from "sonner";
import { useCollectionServers } from "./hooks";
import type { Collection } from "./types";
import { CollectionInstallDialog } from "./CollectionInstallDialog";
import { collectionInstallDisabledReason } from "./install-availability";

export function CollectionCard({
  collection,
}: {
  collection: Collection;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const navigate = useNavigate();
  const organization = useOrganization();
  const { servers, rawServers, isLoading } = useCollectionServers(
    collection.slug,
  );
  const [showInstallDialog, setShowInstallDialog] = useState(false);
  const serverCount = servers.length;
  const detailHref = orgRoutes.collections.detail.href(collection.slug ?? "");

  const installableServers: CatalogServer[] = useMemo(
    () =>
      rawServers.map((server) => ({
        ...server,
        meta: {},
        // Collection-backed servers carry no Pulse DCR metadata.
        supportsDcr: false,
        ...toolStats(server.tools),
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
  const installDisabledReason = collectionInstallDisabledReason({
    isLoading,
    installableServerCount: installableServersWithEndpoint.length,
    projectCount: projects.length,
  });
  const handleInstallClick = (event: React.MouseEvent) => {
    event.stopPropagation();

    if (installableServersWithEndpoint.length === 0) return;

    if (collectionMcpJson.excludedCount > 0) {
      toast.info(
        `Installing ${installableServersWithEndpoint.length} of ${rawServers.length} servers (${collectionMcpJson.excludedCount} ${collectionMcpJson.excludedCount === 1 ? "has" : "have"} no active endpoint).`,
      );
    }

    setShowInstallDialog(true);
  };
  let installButton = (
    <Button
      variant="secondary"
      size="sm"
      className={installDisabledReason ? "pointer-events-none" : undefined}
      disabled={installDisabledReason !== null}
      onClick={handleInstallClick}
    >
      <Button.LeftIcon>
        <Download />
      </Button.LeftIcon>
      <Button.Text>Install</Button.Text>
    </Button>
  );
  if (installDisabledReason) {
    installButton = (
      <SimpleTooltip tooltip={installDisabledReason}>
        <span
          aria-label={`Install unavailable: ${installDisabledReason}`}
          className="focus-visible:ring-ring inline-flex focus-visible:ring-2 focus-visible:outline-none"
          onClick={(event) => event.stopPropagation()}
          tabIndex={0}
        >
          {installButton}
        </span>
      </SimpleTooltip>
    );
  }

  return (
    <Card.Entity
      className="cursor-pointer"
      onClick={() => {
        void navigate(detailHref);
      }}
      icon={
        <LayoutGrid className="text-muted-foreground h-10 w-10 opacity-60" />
      }
      overlay={
        collection.visibility === "private" ? (
          <div className="absolute top-3.5 left-3.5 z-10">
            <Badge
              variant="neutral"
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
          <Text
            variant="subheading"
            as="div"
            className="text-md group-hover:text-primary truncate transition-colors"
            title={collection.name}
          >
            {collection.name}
          </Text>
        </div>
        <Badge variant="neutral">
          <Server className="mr-1 h-3 w-3" />
          {serverCount} {serverCount === 1 ? "server" : "servers"}
        </Badge>
      </div>

      {collection.description && (
        <Text small muted className="mb-3 line-clamp-2">
          {collection.description}
        </Text>
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
            <Text small muted>
              Public
            </Text>
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
        {installButton}
      </div>
      <div onClick={(e) => e.stopPropagation()}>
        <CollectionInstallDialog
          open={showInstallDialog}
          onOpenChange={setShowInstallDialog}
          collectionName={collection.name}
          servers={installableServersWithEndpoint}
          projects={projects}
        />
      </div>
    </Card.Entity>
  );
}
