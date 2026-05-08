import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { Type } from "@/components/ui/type";
import {
  formatRemoteMcpDisplay,
  getRemoteMcpServerArgs,
  remoteMcpRouteParam,
} from "@/lib/sources";
import { useRoutes } from "@/routes";
import type {
  McpServer,
  RemoteMcpServer,
} from "@gram/client/models/components";
import {
  invalidateAllGetRemoteMcpServer,
  invalidateAllRemoteMcpServers,
  useGetRemoteMcpServer,
  useMcpServers,
  useUpdateRemoteMcpServerMutation,
} from "@gram/client/react-query/index.js";
import { Alert, Badge, Button, Dialog, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2, Network, Server, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Navigate, useNavigate, useParams } from "react-router";
import { toast } from "sonner";
import { RemoveRemoteMcpDialogContent } from "./RemoveRemoteMcpDialog";
import { useVerifyRemoteMcpUrl } from "./useVerifyRemoteMcpUrl";
import {
  VerifyRemoteMcpUrlAlert,
  VerifyRemoteMcpUrlButton,
} from "./VerifyRemoteMcpUrlButton";

const VALID_TABS = ["overview", "mcp-servers", "settings"] as const;
type TabValue = (typeof VALID_TABS)[number];

function isValidTab(value: string): value is TabValue {
  return (VALID_TABS as readonly string[]).includes(value);
}

// Mirrors the validation in CreateRemoteMcp.tsx so users get the same feedback
// from the Settings tab. Backend re-validates regardless.
function validateRemoteMcpUrl(value: string): string | null {
  const trimmed = value.trim();
  if (!trimmed) return "URL is required";
  let parsed: URL;
  try {
    parsed = new URL(trimmed);
  } catch {
    return "Enter a valid absolute URL (e.g. https://example.com/mcp)";
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    return "URL must use http or https";
  }
  if (!parsed.hostname) {
    return "URL must include a host";
  }
  return null;
}

export default function RemoteMCPDetails() {
  const { sourceSlug } = useParams<{ sourceSlug: string }>();
  const routes = useRoutes();
  const idOrSlug = sourceSlug ?? "";

  const [activeTab, setActiveTab] = useState<TabValue>(() => {
    const hash = window.location.hash.replace("#", "");
    return isValidTab(hash) ? hash : "overview";
  });

  const handleTabChange = (value: string) => {
    if (!isValidTab(value)) return;
    setActiveTab(value);
    const url = new URL(window.location.href);
    url.hash = value;
    window.history.replaceState(null, "", url.toString());
  };

  const {
    data: remoteMcpServer,
    isLoading,
    isError,
  } = useGetRemoteMcpServer(getRemoteMcpServerArgs(idOrSlug), undefined, {
    enabled: idOrSlug !== "",
  });

  const remoteMcpServerId = remoteMcpServer?.id ?? "";

  const { data: mcpServersResult, isLoading: isLoadingMcpServers } =
    useMcpServers({ remoteMcpServerId }, undefined, {
      enabled: remoteMcpServerId !== "",
    });
  const linkedMcpServers = useMcpServersForRemote(
    mcpServersResult?.mcpServers,
    remoteMcpServerId,
  );

  if (isError || (!isLoading && !remoteMcpServer)) {
    return <Navigate to={routes.sources.href()} replace />;
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={{
            [idOrSlug]: remoteMcpServer
              ? formatRemoteMcpDisplay(remoteMcpServer)
              : undefined,
          }}
          skipSegments={["remotemcp"]}
        />
      </Page.Header>

      <Page.Body
        fullWidth
        noPadding
        fullHeight
        overflowHidden
        className="gap-0"
      >
        <RemoteMcpHero server={remoteMcpServer} />

        <Tabs
          value={activeTab}
          onValueChange={handleTabChange}
          className="flex min-h-0 w-full flex-1 flex-col"
        >
          <div className="shrink-0 border-b">
            <div className="mx-auto max-w-[1270px] px-8">
              <TabsList className="h-auto gap-6 rounded-none bg-transparent p-0">
                <PageTabsTrigger value="overview">Overview</PageTabsTrigger>
                <PageTabsTrigger value="mcp-servers">
                  MCP Servers
                  {linkedMcpServers.length > 0 &&
                    ` (${linkedMcpServers.length})`}
                </PageTabsTrigger>
                <PageTabsTrigger value="settings">Settings</PageTabsTrigger>
              </TabsList>
            </div>
          </div>

          <TabsContent value="overview" className="mt-0 flex-1">
            <OverviewTab
              url={remoteMcpServer?.url}
              transportType={remoteMcpServer?.transportType}
            />
          </TabsContent>

          <TabsContent value="mcp-servers" className="mt-0 flex-1">
            <McpServersTab
              isLoading={isLoadingMcpServers}
              mcpServers={linkedMcpServers}
            />
          </TabsContent>

          <TabsContent value="settings" className="mt-0 flex-1">
            {remoteMcpServer && (
              <SettingsTab
                remoteMcpServerId={remoteMcpServer.id}
                initialName={remoteMcpServer.name ?? ""}
                url={remoteMcpServer.url}
                linkedMcpServers={linkedMcpServers}
              />
            )}
          </TabsContent>
        </Tabs>
      </Page.Body>
    </Page>
  );
}

// `mcpServers.list` is filtered server-side, but applying the same predicate
// client-side is a defensive guard against a stale or unfiltered cache hit
// invalidating the linkage assumption (e.g. another page sharing the cache key
// without the filter). Cheap, removes a class of latent bugs.
function useMcpServersForRemote(
  servers: McpServer[] | undefined,
  remoteMcpServerId: string,
) {
  return useMemo(() => {
    if (!servers || !remoteMcpServerId) return [];
    return servers.filter(
      (server) => server.remoteMcpServerId === remoteMcpServerId,
    );
  }, [servers, remoteMcpServerId]);
}

function RemoteMcpHero({ server }: { server: RemoteMcpServer | undefined }) {
  return (
    <div className="border-b">
      <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
        <Stack gap={2}>
          <Stack direction="horizontal" gap={3} align="center">
            <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-violet-500/10 dark:bg-violet-500/20">
              <Network className="h-5 w-5 text-violet-600 dark:text-violet-400" />
            </div>
            <Heading variant="h1" className="break-all normal-case">
              {server ? formatRemoteMcpDisplay(server) : "Remote MCP server"}
            </Heading>
            <Badge variant="neutral">
              <Badge.Text>Remote MCP</Badge.Text>
            </Badge>
          </Stack>
        </Stack>
      </div>
    </div>
  );
}

function OverviewTab({
  url,
  transportType,
}: {
  url: string | undefined;
  transportType: string | undefined;
}) {
  return (
    <div className="mx-auto w-full max-w-[1270px] space-y-6 px-8 py-8">
      <div>
        <Type muted small className="mb-1">
          URL
        </Type>
        <Type className="font-mono break-all">{url ?? "—"}</Type>
      </div>
      <div>
        <Type muted small className="mb-1">
          Transport Type
        </Type>
        <Type className="font-mono">{transportType ?? "—"}</Type>
      </div>
    </div>
  );
}

function McpServersTab({
  isLoading,
  mcpServers,
}: {
  isLoading: boolean;
  mcpServers: McpServer[];
}) {
  return (
    <div className="mx-auto w-full max-w-[1270px] px-8 py-8">
      {isLoading ? (
        <McpServersSkeleton />
      ) : mcpServers.length > 0 ? (
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
          {mcpServers.map((server) => (
            <McpServerCard key={server.id} server={server} />
          ))}
        </div>
      ) : (
        <div className="py-12 text-center">
          <Server className="text-muted-foreground/50 mx-auto mb-3 h-12 w-12" />
          <Type muted>No MCP servers are linked to this source yet.</Type>
        </div>
      )}
    </div>
  );
}

function McpServersSkeleton() {
  return (
    <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
      {[1, 2, 3].map((i) => (
        <div key={i} className="bg-card animate-pulse rounded-xl border p-6">
          <div className="mb-4 flex items-center gap-3">
            <div className="bg-muted h-10 w-10 rounded-lg" />
            <div className="flex-1">
              <div className="bg-muted mb-2 h-4 w-24 rounded" />
              <div className="bg-muted h-3 w-32 rounded" />
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}

function McpServerCard({ server }: { server: McpServer }) {
  // mcp_servers rows have no name; AGE-2118 introduces broader management.
  // Until then, surface the raw id and visibility so the user can correlate
  // with the underlying record.
  const shortId = server.id.slice(0, 8);
  return (
    <div className="bg-card rounded-xl border p-5">
      <div className="mb-3 flex items-start gap-3">
        <div className="bg-primary/10 flex h-10 w-10 items-center justify-center rounded-lg">
          <Server className="text-primary h-5 w-5" />
        </div>
        <div className="min-w-0 flex-1">
          <Type className="truncate text-base font-semibold" title={server.id}>
            {shortId}…
          </Type>
          <McpServerVisibilityBadge visibility={server.visibility} />
        </div>
      </div>
      <Type small muted>
        Manage MCP server settings (coming soon).
      </Type>
    </div>
  );
}

function McpServerVisibilityBadge({ visibility }: { visibility: string }) {
  const variant: "success" | "neutral" =
    visibility === "public" || visibility === "private" ? "success" : "neutral";
  return (
    <div className="mt-1">
      <Badge variant={variant}>
        <Badge.Text>{visibility}</Badge.Text>
      </Badge>
    </div>
  );
}

function SettingsTab({
  remoteMcpServerId,
  initialName,
  url,
  linkedMcpServers,
}: {
  remoteMcpServerId: string;
  initialName: string;
  url: string;
  linkedMcpServers: McpServer[];
}) {
  return (
    <div className="mx-auto w-full max-w-[1270px] space-y-8 px-8 py-8">
      <NameSection
        remoteMcpServerId={remoteMcpServerId}
        initialName={initialName}
      />
      <UrlSection remoteMcpServerId={remoteMcpServerId} initialUrl={url} />
      <DangerZoneSection
        remoteMcpServerId={remoteMcpServerId}
        url={url}
        linkedMcpServers={linkedMcpServers}
      />
    </div>
  );
}

function NameSection({
  remoteMcpServerId,
  initialName,
}: {
  remoteMcpServerId: string;
  initialName: string;
}) {
  const [draft, setDraft] = useState(initialName);

  // Mirror UrlSection: re-sync the input when the upstream value changes so a
  // stale draft doesn't survive an edit from another tab or a refetch.
  useEffect(() => {
    setDraft(initialName);
  }, [initialName]);

  const queryClient = useQueryClient();
  const update = useUpdateRemoteMcpServerMutation();

  const dirty = draft.trim() !== initialName.trim();
  const saveDisabled = !dirty || update.isPending;

  const handleSave = async () => {
    try {
      await update.mutateAsync({
        request: {
          updateServerForm: {
            id: remoteMcpServerId,
            // Empty string explicitly clears the name on the server side; nil
            // would leave it unchanged. The trimmed input handles whitespace.
            name: draft.trim(),
          },
        },
      });
      await Promise.all([
        invalidateAllGetRemoteMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllRemoteMcpServers(queryClient, { refetchType: "all" }),
      ]);
      toast.success("Remote MCP name updated");
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to update name";
      toast.error(message);
    }
  };

  return (
    <div className="rounded-lg border p-6">
      <Type variant="subheading" className="mb-1">
        Display Name
      </Type>
      <Type muted small className="mb-4">
        Optional name for display purposes such as source listings and
        breadcrumbs. Defaults to URL when empty.
      </Type>
      <Stack gap={2}>
        <Input
          value={draft}
          onChange={(value) => setDraft(value)}
          placeholder="My MCP server"
        />
        {update.isError && (
          <Alert variant="error" dismissible={false}>
            {update.error.message}
          </Alert>
        )}
        <Stack direction="horizontal" gap={2}>
          <RequireScope scope="mcp:write" level="component">
            <Button
              variant="primary"
              disabled={saveDisabled}
              onClick={handleSave}
            >
              {update.isPending ? (
                <>
                  <Button.LeftIcon>
                    <Loader2 className="size-4 animate-spin" />
                  </Button.LeftIcon>
                  <Button.Text>Saving</Button.Text>
                </>
              ) : (
                <Button.Text>Save</Button.Text>
              )}
            </Button>
          </RequireScope>
        </Stack>
      </Stack>
    </div>
  );
}

function UrlSection({
  remoteMcpServerId,
  initialUrl,
}: {
  remoteMcpServerId: string;
  initialUrl: string;
}) {
  const navigate = useNavigate();
  const routes = useRoutes();
  const [draft, setDraft] = useState(initialUrl);
  const [touched, setTouched] = useState(false);

  // When the upstream URL changes (e.g. another tab edited it), reset the
  // local draft so the input reflects the canonical value rather than a stale
  // edit from the previous render.
  useEffect(() => {
    setDraft(initialUrl);
    setTouched(false);
  }, [initialUrl]);

  const queryClient = useQueryClient();
  const update = useUpdateRemoteMcpServerMutation();
  const verify = useVerifyRemoteMcpUrl(draft);

  const validationError = touched ? validateRemoteMcpUrl(draft) : null;
  const dirty = draft.trim() !== initialUrl;
  const saveDisabled =
    !dirty || update.isPending || validateRemoteMcpUrl(draft) !== null;
  const verifyDisabled =
    update.isPending || !draft.trim() || validateRemoteMcpUrl(draft) !== null;

  const handleSave = async () => {
    setTouched(true);
    if (validateRemoteMcpUrl(draft) !== null) return;
    try {
      const updated = await update.mutateAsync({
        request: {
          updateServerForm: {
            id: remoteMcpServerId,
            url: draft.trim(),
          },
        },
      });
      // The server recomputes slug from URL on every update, so a URL change
      // produces a new slug. Replace the route param with the new slug
      // *before* invalidating queries so the refetch uses the new lookup
      // args and the page-level not-found guard doesn't bounce the user back
      // to the Sources index. Replace (not push) avoids a dead history entry
      // pointing at the now-stale slug.
      const nextParam = remoteMcpRouteParam(updated);
      navigate(routes.sources.source.href("remotemcp", nextParam), {
        replace: true,
      });
      // Invalidate every consumer of the remote MCP server: the per-id detail
      // query that drives the hero + Settings inputs, and the project-scoped
      // list query that drives the Sources index card label. refetchType "all"
      // forces the listServers refetch even when Sources isn't mounted so the
      // index reflects the new URL on next visit.
      await Promise.all([
        invalidateAllGetRemoteMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllRemoteMcpServers(queryClient, { refetchType: "all" }),
      ]);
      toast.success("Remote MCP URL updated");
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to update URL";
      toast.error(message);
    }
  };

  return (
    <div className="rounded-lg border p-6">
      <Type variant="subheading" className="mb-1">
        Remote URL
      </Type>
      <Type muted small className="mb-4">
        The endpoint this source proxies to. Must be an absolute http or https
        URL.
      </Type>
      <Stack gap={2}>
        <Input
          value={draft}
          onChange={(value) => {
            setDraft(value);
            if (!touched) setTouched(true);
          }}
          onBlur={() => setTouched(true)}
          placeholder="https://example.com/mcp"
        />
        {validationError && (
          <Alert variant="error" dismissible={false}>
            {validationError}
          </Alert>
        )}
        {update.isError && (
          <Alert variant="error" dismissible={false}>
            {update.error.message}
          </Alert>
        )}
        <VerifyRemoteMcpUrlAlert state={verify} />
        <Stack direction="horizontal" gap={2}>
          <RequireScope scope="mcp:write" level="component">
            <VerifyRemoteMcpUrlButton
              state={verify}
              url={draft}
              disabled={verifyDisabled}
            />
          </RequireScope>
          <RequireScope scope="mcp:write" level="component">
            <Button
              variant="primary"
              disabled={saveDisabled}
              onClick={handleSave}
            >
              {update.isPending ? (
                <>
                  <Button.LeftIcon>
                    <Loader2 className="size-4 animate-spin" />
                  </Button.LeftIcon>
                  <Button.Text>Saving</Button.Text>
                </>
              ) : (
                <Button.Text>Save</Button.Text>
              )}
            </Button>
          </RequireScope>
        </Stack>
      </Stack>
    </div>
  );
}

function DangerZoneSection({
  remoteMcpServerId,
  url,
  linkedMcpServers,
}: {
  remoteMcpServerId: string;
  url: string;
  linkedMcpServers: McpServer[];
}) {
  const navigate = useNavigate();
  const routes = useRoutes();
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);

  return (
    <div className="border-destructive/30 rounded-lg border p-6">
      <Type variant="subheading" className="text-destructive mb-1">
        Danger Zone
      </Type>
      <Type muted small className="mb-4">
        Deleting this source will also remove the linked MCP servers and their
        endpoints. This action cannot be undone.
      </Type>
      <RequireScope scope="mcp:write" level="component">
        <Button
          variant="destructive-primary"
          size="md"
          onClick={() => setDeleteDialogOpen(true)}
        >
          <Button.LeftIcon>
            <Trash2 className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>Delete Source</Button.Text>
        </Button>
      </RequireScope>

      <Dialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <Dialog.Content className="max-w-2xl!">
          <RemoveRemoteMcpDialogContent
            remoteMcpServerId={remoteMcpServerId}
            url={url}
            linkedMcpServers={linkedMcpServers}
            onClose={() => setDeleteDialogOpen(false)}
            onSuccess={() => {
              setDeleteDialogOpen(false);
              navigate(routes.sources.href());
            }}
          />
        </Dialog.Content>
      </Dialog>
    </div>
  );
}
