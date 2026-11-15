import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
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
import { Text } from "@/components/ui/Text";
import { useOrganization, useProject } from "@/contexts/Auth";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useLocalStorageState } from "@/hooks/useLocalStorageState";
import { mcpServerRouteParam } from "@/lib/sources";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import type { ProbeURLResult } from "@gram/client/models/components/probeurlresult.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useProbeRemoteMcpURLMutation } from "@gram/client/react-query/probeRemoteMcpURL.js";
import { useEffect, useId, useRef, useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import {
  onboardReadiness,
  SHADOW_MCP_OFFICIAL_STATE_LABEL,
  type OnboardCheck,
  shadowMCPOfficialServerDescription,
  shadowMCPOnboardHref,
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
 * How a Shadow MCP server becomes one the organization serves itself.
 *
 * Three states, one slot: the server is already official (where it is
 * served), it can be made official (the checks passed, one button), or it
 * cannot yet (what is in the way, and a request for help). The checks run as
 * the panel opens, so the admin reads an answer rather than starting a test.
 */
export function OfficialServerPanel({
  server,
}: {
  server: ShadowMCPInventoryServer;
}): JSX.Element | null {
  // A local command has no URL to serve; it never reaches this page, and
  // the guard keeps the panel honest if that changes.
  if (server.targetKind === "stdio_command") return null;
  if (server.officialServer) return <OnboardedPanel server={server} />;
  return <MakeOfficialPanel server={server} />;
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

function OnboardedPanel({ server }: { server: ShadowMCPInventoryServer }) {
  const routes = useRoutes();
  const official = server.officialServer;
  if (!official) return null;

  return (
    <section aria-label="Official server" className={PANEL_CLASS}>
      <div className="flex flex-wrap items-start justify-between gap-4">
        <PanelHeading
          title="Official server"
          description={shadowMCPOfficialServerDescription(server)}
          badge={
            official.directAccessAllowed ? (
              <Badge variant="warning" size="sm">
                <Badge.Text>
                  {SHADOW_MCP_OFFICIAL_STATE_LABEL.testing}
                </Badge.Text>
              </Badge>
            ) : (
              <Badge variant="success" size="sm">
                <Badge.Text>
                  {SHADOW_MCP_OFFICIAL_STATE_LABEL.onboarded}
                </Badge.Text>
              </Badge>
            )
          }
        />
        <Button variant="secondary" size="sm" asChild>
          <Link
            to={routes.mcp.details.overview.href(
              mcpServerRouteParam({
                id: official.mcpServerId,
                slug: official.slug,
              }),
            )}
          >
            <Button.Text>Open official server</Button.Text>
          </Link>
        </Button>
      </div>
      {official.url ? (
        <div className="space-y-1">
          <p className="text-eyebrow">Where agents connect</p>
          <div className="flex min-w-0 items-center gap-1">
            <span className="truncate font-mono text-sm" title={official.url}>
              {official.url}
            </span>
            <CopyButton text={official.url} size="xs" tooltip="Copy URL" />
          </div>
        </div>
      ) : null}
      <OfficialServerAdvanced server={server} />
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

function MakeOfficialPanel({ server }: { server: ShadowMCPInventoryServer }) {
  const routes = useRoutes();
  const organization = useOrganization();
  const project = useProject();
  const telemetry = useTelemetry();
  const probe = useProbeRemoteMcpURLMutation();
  const [result, setResult] = useState<ProbeURLResult | null>(null);
  const [checkFailed, setCheckFailed] = useState(false);
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

  const requestHelp = () => {
    // The URL is the canonical form the inventory already shows: no query
    // string and no credentials. Staff need it to set the server up.
    telemetry.capture(ONBOARDING_REQUEST_EVENT, {
      organization_slug: organization.slug,
      project_slug: project.slug,
      server_url: server.canonicalServerUrl,
      server_name: serverName,
      blocker: readiness?.blockerCode ?? "check_failed",
      people_using_it: server.userCount,
      calls: server.observedUseCount,
    });
    setRequested(true);
    toast.success("Request sent. Speakeasy will contact you to set it up.");
  };

  return (
    <section aria-label="Make this server official" className={PANEL_CLASS}>
      <div className="flex flex-wrap items-start justify-between gap-4">
        <PanelHeading
          title="Make this an official server"
          description="Serve it through Speakeasy so you decide who may use it and can see what it does. Direct connections are then blocked, and agents are sent to the official server."
        />
        {readiness?.ready ? (
          <Button size="sm" asChild>
            <Link
              to={shadowMCPOnboardHref(routes.mcp.add.remote.href(), server)}
            >
              <Button.Text>Make official</Button.Text>
            </Link>
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

      {readiness && !readiness.ready ? (
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
            <Button variant="tertiary" size="sm" asChild>
              <Link
                to={shadowMCPOnboardHref(routes.mcp.add.remote.href(), server)}
              >
                <Button.Text>Set it up myself</Button.Text>
              </Link>
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
      <OfficialServerAdvanced server={server} />
    </section>
  );
}

/** The select value for "the server found by URL", which no server id has. */
const FOUND_BY_URL = "found-by-url";

/**
 * Escape hatches for an admin who knows better than the default: name any
 * server as the official version, or keep direct connections working while
 * the official server is tested. Closed by default, since most admins never
 * need either, and saved with its own button because both change what
 * agents may reach.
 */
function OfficialServerAdvanced({
  server,
}: {
  server: ShadowMCPInventoryServer;
}): JSX.Element {
  const official = server.officialServer;
  const gramProject = useProjectSlugForRequests();
  const checkboxId = useId();
  const [open, setOpen] = useState(false);
  const serversQuery = useMcpServers({ gramProject }, undefined, {
    enabled: open,
    throwOnError: false,
  });
  const { save, isPending } = useSaveShadowMCPOfficialServer();

  const savedServer = official?.chosen ? official.mcpServerId : FOUND_BY_URL;
  const savedKeepDirect = official?.directAccessAllowed ?? false;
  const [selected, setSelected] = useState(savedServer);
  const [keepDirect, setKeepDirect] = useState(savedKeepDirect);
  // A save, or a change made elsewhere, resets the form to what is stored.
  useEffect(() => {
    setSelected(savedServer);
    setKeepDirect(savedKeepDirect);
  }, [savedServer, savedKeepDirect]);

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
  // Without a server found by URL, the default is "no official server", and
  // there is nothing whose direct connections could be kept.
  const foundByURLLabel =
    official && !official.chosen
      ? `${official.name} (serves this URL)`
      : "None";
  const hasOfficial =
    selected !== FOUND_BY_URL || (!!official && !official.chosen);
  const dirty = selected !== savedServer || keepDirect !== savedKeepDirect;

  const onSave = async () => {
    try {
      await save({
        serverUrl: server.canonicalServerUrl,
        mcpServerId: selected === FOUND_BY_URL ? undefined : selected,
        allowDirectAccess: hasOfficial && keepDirect,
      });
      toast.success("Official server settings saved");
    } catch {
      toast.error("Could not save. Try again.");
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
      <CollapsibleContent className="mt-4 max-w-xl space-y-5">
        <div className="space-y-1.5">
          <Text variant="small" className="font-medium">
            Official server
          </Text>
          <Text muted small>
            Choose any server in this project to stand in for this one. Agents
            are sent to it when a direct connection is blocked.
          </Text>
          <Select value={selected} onValueChange={setSelected}>
            <SelectTrigger aria-label="Official server" className="w-full">
              <SelectValue placeholder="Choose a server" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={FOUND_BY_URL}>{foundByURLLabel}</SelectItem>
              {candidates.map((candidate) => (
                <SelectItem key={candidate.id} value={candidate.id}>
                  {candidate.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="flex items-start gap-2.5">
          <Checkbox
            id={checkboxId}
            className="mt-0.5"
            checked={hasOfficial && keepDirect}
            disabled={!hasOfficial}
            onCheckedChange={(checked) => setKeepDirect(checked === true)}
          />
          <label htmlFor={checkboxId} className="space-y-0.5">
            <span className="block text-sm">
              Keep direct connections working
            </span>
            <Text muted small>
              {hasOfficial
                ? "Use this while you test the official server. Direct connections then follow the policy's usual rules instead of being blocked."
                : "Available once this server has an official version."}
            </Text>
          </label>
        </div>
        <Button
          variant="secondary"
          size="sm"
          disabled={!dirty || isPending}
          onClick={() => void onSave()}
        >
          <Button.Text>{isPending ? "Saving" : "Save"}</Button.Text>
        </Button>
      </CollapsibleContent>
    </Collapsible>
  );
}
