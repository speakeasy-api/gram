import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/Collapsible";
import { CopyButton } from "@/components/ui/CopyButton";
import { Icon } from "@/components/ui/Icon";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Skeleton } from "@/components/ui/Skeleton";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { useOrganization, useProject } from "@/contexts/Auth";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useLocalStorageState } from "@/hooks/useLocalStorageState";
import { mcpServerRouteParam } from "@/lib/sources";
import { cn } from "@/lib/utils";
import { type CatalogServer, useListMCPCatalog } from "@/pages/catalog/hooks";
import { useRoutes } from "@/routes";
import type { ProbeURLResult } from "@gram/client/models/components/probeurlresult.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useProbeRemoteMcpURLMutation } from "@gram/client/react-query/probeRemoteMcpURL.js";
import { ExternalLink } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router";
import { toast } from "sonner";
import { MakeOfficialSheet } from "./MakeOfficialSheet";
import {
  catalogCanHelpWith,
  onboardReadiness,
  SHADOW_MCP_MAKE_OFFICIAL_PARAM,
  SHADOW_MCP_OFFICIAL_STATE_LABEL,
  type OnboardCheck,
  shadowMCPCatalogMatch,
  shadowMCPOfficialServerDescription,
} from "./shadowMCPOfficialServer";
import { useSaveShadowMCPOfficialServer } from "./useSaveShadowMCPOfficialServer";

/**
 * The event a request for help records. Speakeasy staff are notified from
 * it and set the server up by hand, so it is the only delivery there is:
 * renaming it breaks that notification.
 */
const ONBOARDING_REQUEST_EVENT = "shadow_mcp_onboarding_requested";

const PANEL_CLASS = "border-border bg-card space-y-4 border p-4";

/**
 * How a Shadow MCP server is linked to the version the organization serves
 * itself.
 *
 * One slot, three states: it is linked (switch the link off and on, open
 * the official server, or remove the link), the project already serves the
 * same URL (link it), or neither (check it, then make it official in a
 * sheet, or say what is in the way).
 */
export function OfficialServerPanel({
  server,
}: {
  server: ShadowMCPInventoryServer;
}): JSX.Element | null {
  // A local command has no URL to serve; it never reaches this page, and
  // the guard keeps the panel honest if that changes.
  if (server.targetKind === "stdio_command") return null;
  if (server.officialServer) return <LinkedPanel server={server} />;
  return <UnlinkedPanel server={server} />;
}

function PanelHeading({
  title,
  description,
  badge,
}: {
  title: string;
  description: string;
  badge?: React.ReactNode;
}) {
  return (
    <div className="min-w-0 space-y-1">
      <div className="flex flex-wrap items-center gap-2">
        <Text variant="subheading">{title}</Text>
        {badge}
      </div>
      <Text muted small className="max-w-2xl">
        {description}
      </Text>
    </div>
  );
}

/** Opens an MCP server's own page in a new tab, to inspect and try it. */
function OpenServerButton({
  mcpServerId,
  slug,
  label = "Open",
}: {
  mcpServerId: string;
  slug?: string;
  label?: string;
}) {
  const routes = useRoutes();
  return (
    <Button variant="secondary" size="sm" asChild>
      <a
        href={routes.mcp.details.overview.href(
          mcpServerRouteParam({ id: mcpServerId, slug }),
        )}
        target="_blank"
        rel="noreferrer"
      >
        <Button.Text>{label}</Button.Text>
        <Button.RightIcon>
          <ExternalLink className="size-3.5" aria-hidden />
        </Button.RightIcon>
      </a>
    </Button>
  );
}

function LinkedPanel({ server }: { server: ShadowMCPInventoryServer }) {
  const { save, isPending } = useSaveShadowMCPOfficialServer();
  const official = server.officialServer;
  if (!official) return null;
  const linked = !official.directAccessAllowed;

  const setLinked = async (next: boolean) => {
    try {
      await save({
        serverUrl: server.canonicalServerUrl,
        mcpServerId: official.mcpServerId,
        allowDirectAccess: !next,
      });
      toast.success(
        next
          ? "Linked. Direct connections are blocked again."
          : "Unlinked. Direct connections follow the usual rules.",
      );
    } catch {
      toast.error("Could not change the link. Try again.");
    }
  };

  const removeLink = async () => {
    try {
      await save({
        serverUrl: server.canonicalServerUrl,
        mcpServerId: undefined,
        allowDirectAccess: false,
      });
      toast.success(`Link to “${official.name}” removed`, {
        action: {
          label: "Undo",
          onClick: () =>
            void save({
              serverUrl: server.canonicalServerUrl,
              mcpServerId: official.mcpServerId,
              allowDirectAccess: official.directAccessAllowed,
            }),
        },
      });
    } catch {
      toast.error("Could not remove the link. Try again.");
    }
  };

  return (
    <section aria-label="Official server" className={PANEL_CLASS}>
      <PanelHeading
        title="Official server"
        description={shadowMCPOfficialServerDescription(server)}
        badge={
          <Badge variant={linked ? "success" : "warning"} size="sm">
            <Badge.Text>
              {linked
                ? SHADOW_MCP_OFFICIAL_STATE_LABEL.onboarded
                : SHADOW_MCP_OFFICIAL_STATE_LABEL.unlinked}
            </Badge.Text>
          </Badge>
        }
      />

      <div className="border-border flex flex-wrap items-center justify-between gap-4 border p-3">
        <div className="flex min-w-0 items-center gap-3">
          <Icon
            name="server"
            className="text-muted-foreground size-4 shrink-0"
          />
          <div className="min-w-0">
            <Text variant="small" className="truncate font-medium">
              {official.name}
            </Text>
            {official.url ? (
              <div className="flex min-w-0 items-center gap-1">
                <span
                  className="text-muted-foreground truncate font-mono text-xs"
                  title={official.url}
                >
                  {official.url}
                </span>
                <CopyButton text={official.url} size="xs" tooltip="Copy URL" />
              </div>
            ) : (
              <Text muted small className="text-xs">
                No address yet
              </Text>
            )}
          </div>
        </div>
        <div className="flex items-center gap-4">
          <label className="flex items-center gap-2 text-sm">
            <Switch
              checked={linked}
              disabled={isPending}
              onCheckedChange={(next) => void setLinked(next)}
              aria-label="Linked"
            />
            Linked
          </label>
          <OpenServerButton
            mcpServerId={official.mcpServerId}
            slug={official.slug}
          />
        </div>
      </div>

      <div className="flex flex-wrap items-center justify-between gap-3">
        <Text muted small className="max-w-xl">
          {linked
            ? "Switch the link off to let direct connections work while you test the official server."
            : "Switch the link on when the official server is ready."}
        </Text>
        <Button
          variant="tertiary"
          size="sm"
          disabled={isPending}
          onClick={() => void removeLink()}
        >
          <Button.Text>Remove link</Button.Text>
        </Button>
      </div>
    </section>
  );
}

function CheckRow({ check }: { check: OnboardCheck }) {
  return (
    <li className="flex items-start gap-2">
      <Icon
        name={
          check.state === "pass"
            ? "circle-check"
            : check.state === "fail"
              ? "circle-alert"
              : "circle-dashed"
        }
        className={cn(
          "mt-0.5 size-4 shrink-0",
          check.state === "pass" && "text-default-success",
          check.state === "fail" && "text-default-warning",
          check.state === "skipped" && "text-muted-foreground",
        )}
      />
      <div className="min-w-0">
        <Text
          variant="small"
          className={cn(check.state === "skipped" && "text-muted-foreground")}
        >
          {check.label}
          {check.state === "skipped" ? " (not checked)" : ""}
        </Text>
        {check.detail ? (
          <Text muted small className="text-xs">
            {check.detail}
          </Text>
        ) : null}
      </div>
    </li>
  );
}

function ChecksSkeleton() {
  return (
    <ul className="space-y-2" aria-label="Checking the server">
      {[0, 1, 2].map((row) => (
        <li key={row} className="flex items-center gap-2">
          <Skeleton className="size-4" />
          <Skeleton className="h-4 w-56" />
        </li>
      ))}
    </ul>
  );
}

function UnlinkedPanel({ server }: { server: ShadowMCPInventoryServer }) {
  const suggested = server.suggestedOfficialServer;
  return (
    <section aria-label="Make this server official" className={PANEL_CLASS}>
      {suggested ? (
        <SuggestedLink server={server} />
      ) : (
        <MakeOfficial server={server} />
      )}
      <LinkAnotherServer server={server} />
    </section>
  );
}

/**
 * The project already serves this URL, so making it official is one click:
 * no new server, no setup.
 */
function SuggestedLink({ server }: { server: ShadowMCPInventoryServer }) {
  const { save, isPending } = useSaveShadowMCPOfficialServer();
  const suggested = server.suggestedOfficialServer;
  if (!suggested) return null;

  const linkIt = async () => {
    try {
      await save({
        serverUrl: server.canonicalServerUrl,
        mcpServerId: suggested.mcpServerId,
        allowDirectAccess: false,
      });
      toast.success(`Linked to “${suggested.name}”`);
    } catch {
      toast.error("Could not link the server. Try again.");
    }
  };

  return (
    <>
      <PanelHeading
        title="Make this an official server"
        description="Link it to the version Speakeasy serves, so you decide who may use it and can see what it does. Direct connections are then blocked, and agents are sent to the official server."
      />
      <div className="border-border flex flex-wrap items-center justify-between gap-4 border p-3">
        <div className="flex min-w-0 items-start gap-3">
          <Icon
            name="link"
            className="text-muted-foreground mt-0.5 size-4 shrink-0"
          />
          <div className="min-w-0 space-y-0.5">
            <Text variant="small" className="font-medium">
              {`Already served as “${suggested.name}”`}
            </Text>
            <Text muted small className="max-w-xl">
              A server in this project proxies the same URL. Link it to make it
              the official version.
            </Text>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <OpenServerButton
            mcpServerId={suggested.mcpServerId}
            slug={suggested.slug}
          />
          <Button size="sm" disabled={isPending} onClick={() => void linkIt()}>
            <Button.Text>{isPending ? "Linking" : "Link it"}</Button.Text>
          </Button>
        </div>
      </div>
    </>
  );
}

function MakeOfficial({ server }: { server: ShadowMCPInventoryServer }) {
  const routes = useRoutes();
  const organization = useOrganization();
  const project = useProject();
  const telemetry = useTelemetry();
  const probe = useProbeRemoteMcpURLMutation();
  const [result, setResult] = useState<ProbeURLResult | null>(null);
  const [checkFailed, setCheckFailed] = useState(false);
  const [searchParams, setSearchParams] = useSearchParams();
  const [sheetOpen, setSheetOpen] = useState(false);
  // Keyed by project and server so a request sent for one server never
  // reads as sent for another. It only remembers that this browser asked;
  // the request itself was delivered when the button was pressed.
  const [requested, setRequested] = useLocalStorageState<boolean>(
    `shadow-mcp-onboarding-requested:${project.id}:${server.serverSlug}`,
    false,
  );
  const url = server.canonicalServerUrl;
  const { mutateAsync } = probe;
  const latestUrl = useRef(url);
  latestUrl.current = url;

  const runChecks = (target: string) => {
    setResult(null);
    setCheckFailed(false);
    mutateAsync({ request: { probeURLForm: { url: target } } })
      .then((response) => {
        if (latestUrl.current !== target) return;
        setResult(response);
      })
      .catch(() => {
        if (latestUrl.current !== target) return;
        setCheckFailed(true);
      });
  };

  // The checks answer "can this be made official?", which is why the admin
  // opened the page, so they run without being asked. Once per server URL.
  useEffect(() => {
    runChecks(url);
    // runChecks closes over setters and a stable mutateAsync only.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url]);

  const readiness = result ? onboardReadiness(result) : null;
  const serverName =
    server.serverName || server.urlHost || server.canonicalServerUrl;

  // The list's "Make official" lands here with the sheet asked for. It opens
  // once the checks pass, and the flag is dropped so a reload does not
  // reopen it.
  const wantsSheet = searchParams.get(SHADOW_MCP_MAKE_OFFICIAL_PARAM) === "1";
  useEffect(() => {
    if (!wantsSheet || !readiness) return;
    if (readiness.ready) setSheetOpen(true);
    const next = new URLSearchParams(searchParams);
    next.delete(SHADOW_MCP_MAKE_OFFICIAL_PARAM);
    setSearchParams(next, { replace: true });
  }, [wantsSheet, readiness, searchParams, setSearchParams]);

  // A server whose sign-in needs a hand-registered client may still be in the
  // catalog, whose page walks through that setup. The catalog is only read
  // when it can help.
  const catalogCanHelp =
    readiness !== null &&
    !readiness.ready &&
    catalogCanHelpWith(readiness.blockerCode);
  const catalog = useListMCPCatalog(undefined, undefined, {
    enabled: catalogCanHelp,
  });
  const catalogEntry =
    catalogCanHelp && catalog.data?.servers
      ? shadowMCPCatalogMatch(catalog.data.servers as CatalogServer[], url)
      : null;
  const catalogPending = catalogCanHelp && catalog.isLoading;

  const requestHelp = () => {
    // The URL is the canonical form the inventory already shows: no query
    // string and no credentials. Staff need it to set the server up.
    telemetry.capture(ONBOARDING_REQUEST_EVENT, {
      organization_slug: organization.slug,
      project_slug: project.slug,
      server_url: server.canonicalServerUrl,
      server_name: serverName,
      blocker: readiness?.blockerCode ?? "check_failed",
      catalog_specifier: catalogEntry?.registrySpecifier ?? null,
      people_using_it: server.userCount,
      calls: server.observedUseCount,
    });
    setRequested(true);
    toast.success("Request sent. Speakeasy will contact you to set it up.");
  };

  return (
    <>
      <div className="flex flex-wrap items-start justify-between gap-4">
        <PanelHeading
          title="Make this an official server"
          description="Serve it through Speakeasy so you decide who may use it and can see what it does. Direct connections are then blocked, and agents are sent to the official server."
        />
        {readiness?.ready ? (
          <Button size="sm" onClick={() => setSheetOpen(true)}>
            <Button.Text>Make official</Button.Text>
          </Button>
        ) : null}
      </div>

      {probe.isPending && !result ? <ChecksSkeleton /> : null}

      {readiness ? (
        <ul className="space-y-2">
          {readiness.checks.map((check) => (
            <CheckRow key={check.id} check={check} />
          ))}
        </ul>
      ) : null}

      {checkFailed ? (
        <div className="flex flex-wrap items-center gap-3">
          <Text muted small>
            The server could not be checked. Try again.
          </Text>
          <Button variant="secondary" size="sm" onClick={() => runChecks(url)}>
            <Button.Text>Check again</Button.Text>
          </Button>
        </div>
      ) : null}

      {readiness && !readiness.ready && catalogEntry ? (
        <div className="border-border flex flex-wrap items-center justify-between gap-3 border-t pt-4">
          <div className="flex min-w-0 items-start gap-3">
            <Icon
              name="book-open"
              className="text-muted-foreground mt-0.5 size-4 shrink-0"
            />
            <div className="min-w-0 space-y-1">
              <Text variant="small" className="font-medium">
                Set it up from the Speakeasy catalog
              </Text>
              <Text muted small className="max-w-xl">
                {`${catalogEntry.title || serverName} is in the catalog. Add it from there, then enter the client you registered with its sign-in provider. Once it is on, link it here.`}
              </Text>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="secondary"
              size="sm"
              disabled={requested}
              onClick={requestHelp}
            >
              <Button.Text>
                {requested ? "Help requested" : "Request help"}
              </Button.Text>
            </Button>
            <Button size="sm" asChild>
              <Link
                to={routes.mcp.catalog.detail.href(
                  encodeURIComponent(catalogEntry.registrySpecifier),
                )}
              >
                <Button.Text>Set up from the catalog</Button.Text>
              </Link>
            </Button>
          </div>
        </div>
      ) : null}

      {readiness && !readiness.ready && !catalogEntry && !catalogPending ? (
        <div className="border-border flex flex-wrap items-center justify-between gap-3 border-t pt-4">
          <div className="min-w-0 space-y-1">
            <Text variant="small" className="font-medium">
              This server needs a manual setup
            </Text>
            <Text muted small className="max-w-xl">
              {requested
                ? "Your request is with Speakeasy. We will contact you to set it up."
                : "It cannot be made official automatically. Ask Speakeasy and we will set it up with you."}
            </Text>
          </div>
          <div className="flex items-center gap-2">
            <Button
              variant="tertiary"
              size="sm"
              onClick={() => setSheetOpen(true)}
            >
              <Button.Text>Set it up myself</Button.Text>
            </Button>
            <Button
              variant="secondary"
              size="sm"
              disabled={requested}
              onClick={requestHelp}
            >
              <Button.Text>
                {requested ? "Help requested" : "Request help"}
              </Button.Text>
            </Button>
          </div>
        </div>
      ) : null}

      <MakeOfficialSheet
        server={server}
        probe={result}
        open={sheetOpen}
        onOpenChange={setSheetOpen}
      />
    </>
  );
}

/**
 * The escape hatch for an admin who knows better than the suggestion: link
 * any server in the project as the official version. Closed by default,
 * since most admins never need it.
 */
function LinkAnotherServer({
  server,
}: {
  server: ShadowMCPInventoryServer;
}): JSX.Element {
  const gramProject = useProjectSlugForRequests();
  const [open, setOpen] = useState(false);
  const serversQuery = useMcpServers({ gramProject }, undefined, {
    enabled: open,
    throwOnError: false,
  });
  const { save, isPending } = useSaveShadowMCPOfficialServer();
  const [selected, setSelected] = useState("");

  const enabled = (serversQuery.data?.mcpServers ?? []).filter(
    (candidate) => candidate.visibility !== "disabled",
  );
  const nameCounts = new Map<string, number>();
  for (const candidate of enabled) {
    const name = candidate.name || candidate.slug || "Unnamed MCP server";
    nameCounts.set(name, (nameCounts.get(name) ?? 0) + 1);
  }
  // Two servers can share a name; the slug tells them apart.
  const candidates = enabled
    .map((candidate) => {
      const name = candidate.name || candidate.slug || "Unnamed MCP server";
      const repeated = (nameCounts.get(name) ?? 0) > 1 && candidate.slug;
      return {
        id: candidate.id,
        label: repeated ? `${name} · ${candidate.slug}` : name,
      };
    })
    .sort((a, b) => a.label.localeCompare(b.label));

  const onLink = async () => {
    const chosen = candidates.find((candidate) => candidate.id === selected);
    try {
      await save({
        serverUrl: server.canonicalServerUrl,
        mcpServerId: selected,
        allowDirectAccess: false,
      });
      toast.success(`Linked to “${chosen?.label ?? "the server"}”`);
    } catch {
      toast.error("Could not link the server. Try again.");
    }
  };

  return (
    <Collapsible
      open={open}
      onOpenChange={setOpen}
      className="border-border border-t pt-3"
    >
      <CollapsibleTrigger className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-sm transition-colors [&[data-state=open]>svg]:rotate-90">
        <Icon name="chevron-right" className="size-3.5" aria-hidden />
        Advanced
      </CollapsibleTrigger>
      <CollapsibleContent className="mt-4 max-w-xl space-y-1.5">
        <Text variant="small" className="font-medium">
          Link another server
        </Text>
        <Text muted small>
          Link any server in this project as the official version, for example
          one at a different URL. Agents are sent to it when a direct connection
          is blocked.
        </Text>
        <div className="flex items-center gap-2 pt-1">
          <Select value={selected} onValueChange={setSelected}>
            <SelectTrigger aria-label="Server to link" className="w-full">
              <SelectValue placeholder="Choose a server" />
            </SelectTrigger>
            <SelectContent>
              {candidates.map((candidate) => (
                <SelectItem key={candidate.id} value={candidate.id}>
                  {candidate.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button
            variant="secondary"
            size="sm"
            disabled={selected === "" || isPending}
            onClick={() => void onLink()}
          >
            <Button.Text>{isPending ? "Linking" : "Link"}</Button.Text>
          </Button>
        </div>
      </CollapsibleContent>
    </Collapsible>
  );
}
