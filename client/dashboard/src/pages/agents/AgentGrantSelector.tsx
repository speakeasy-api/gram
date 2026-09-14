import {
  ToolSelectionPanel,
  type ToolSelectionServer,
} from "@/components/tool-selection/ToolSelectionPanel";
import type { ToolAnnotation } from "@/components/tool-selection/annotations";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useToolMetadata } from "@/hooks/useToolMetadata";
import { useOrgMcpServers } from "@/pages/access/useOrgMcpServers";
import { toolMetadataToServerTools } from "@/pages/access/remoteToolMetadata";
import type { Server } from "@/pages/access/serverMerge";
import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import { useMemo, type JSX } from "react";

import {
  ANY_RESOURCE,
  DISPOSITION_LABELS,
  canNarrowResource,
  delegableGrantKey,
  openDimensions,
  resourceInventoryFor,
  type GrantNarrowing,
} from "./agent-api-key-grants";

/** Narrowing per selected candidate, keyed by `delegableGrantKey`. */
export type GrantNarrowings = Record<string, GrantNarrowing>;

interface ResourceOption {
  id: string;
  name: string;
  group?: string;
  /** Owning project, for keeping a server and a project choice compatible. */
  projectId?: string;
}

/**
 * A server plus the project context `useToolMetadata` needs: the endpoint
 * hard-scopes stored tool metadata to the `Gram-Project` header, and this
 * editor lists servers across every project.
 */
interface ServerEntry {
  server: Server;
  projectId: string;
  projectSlug: string | undefined;
}

const SELECT_CLASS = "border-input bg-background block h-9 w-full border px-2";

export function AgentGrantSelector({
  grants,
  narrowings,
  onChange,
  disabled,
}: {
  grants: AgentPolicyGrantForm[];
  narrowings: GrantNarrowings;
  onChange: (next: GrantNarrowings) => void;
  disabled?: boolean;
}): JSX.Element {
  const organization = useOrganization();
  const needsServers = grants.some(
    (grant) => grant.selector.resourceKind === "mcp",
  );
  const servers = useOrgMcpServers(needsServers);

  const serverIndex = useMemo(() => {
    const slugByProject = new Map(
      (organization.projects ?? []).map((project) => [
        project.id,
        project.slug,
      ]),
    );
    const entries = new Map<string, ServerEntry>();
    for (const group of servers.groups) {
      for (const server of group.servers) {
        entries.set(server.id, {
          server,
          projectId: group.projectId,
          projectSlug: slugByProject.get(group.projectId),
        });
      }
    }
    return entries;
  }, [servers.groups, organization.projects]);

  const projectOptions = useMemo(
    (): ResourceOption[] =>
      (organization.projects ?? []).map((project) => ({
        id: project.id,
        name: project.name,
      })),
    [organization.projects],
  );
  const serverOptions = useMemo(
    (): ResourceOption[] =>
      servers.groups.flatMap((group) =>
        group.servers.map((server) => ({
          id: server.id,
          name: server.name,
          group: group.projectName,
          projectId: group.projectId,
        })),
      ),
    [servers.groups],
  );

  if (grants.length === 0)
    return (
      <Text muted>
        No permissions can be delegated to this agent right now. A credential
        can only carry permissions the agent policy, the owner and you all hold,
        and permissions shaped by an exception are left out.
      </Text>
    );

  const toggle = (key: string, checked: boolean) => {
    const next = { ...narrowings };
    if (checked) next[key] = {};
    else delete next[key];
    onChange(next);
  };

  return (
    <fieldset className="space-y-3">
      <legend>Delegable permissions</legend>
      <Text small muted>
        These are the permissions the agent policy, the owner and you all hold.
        Each is delegated exactly as listed unless you narrow it here, and
        nothing is selected by default.
      </Text>
      {grants.map((grant) => {
        const key = delegableGrantKey(grant);
        return (
          <GrantRow
            key={key}
            grant={grant}
            narrowing={narrowings[key]}
            disabled={disabled}
            projectOptions={projectOptions}
            serverOptions={serverOptions}
            serversSettled={servers.settled}
            serversFailed={servers.isError}
            onRetryServers={servers.refetch}
            serverIndex={serverIndex}
            onToggle={(checked) => toggle(key, checked)}
            onNarrow={(narrowing) =>
              onChange({ ...narrowings, [key]: narrowing })
            }
          />
        );
      })}
    </fieldset>
  );
}

function GrantRow({
  grant,
  narrowing,
  disabled,
  projectOptions,
  serverOptions,
  serversSettled,
  serversFailed,
  onRetryServers,
  serverIndex,
  onToggle,
  onNarrow,
}: {
  grant: AgentPolicyGrantForm;
  narrowing: GrantNarrowing | undefined;
  disabled?: boolean;
  projectOptions: ResourceOption[];
  serverOptions: ResourceOption[];
  serversSettled: boolean;
  serversFailed: boolean;
  onRetryServers: () => void;
  serverIndex: Map<string, ServerEntry>;
  onToggle: (checked: boolean) => void;
  onNarrow: (narrowing: GrantNarrowing) => void;
}): JSX.Element {
  const { selector } = grant;
  const inventory = resourceInventoryFor(selector.resourceKind);
  const isMcp = inventory === "mcp";
  // An mcp inventory that has not resolved yet must not read as empty, so the
  // resource choice stays hidden until both org listings settle.
  const mcpOptions = serversSettled ? serverOptions : [];
  const open = new Set(openDimensions(grant));
  const selected = narrowing !== undefined;
  const resolvedResourceId = narrowing?.resourceId ?? selector.resourceId;
  const entry = serverIndex.get(resolvedResourceId);
  // Existing project ceilings constrain inventory, but this editor never adds one.
  const projectFilter =
    selector.projectId !== ANY_RESOURCE ? selector.projectId : undefined;
  const serversInProject = projectFilter
    ? mcpOptions.filter((option) => option.projectId === projectFilter)
    : mcpOptions;
  const resourceOptions = isMcp ? serversInProject : projectOptions;

  const update = (patch: GrantNarrowing) =>
    onNarrow({ ...narrowing, ...patch });

  return (
    <div className="border-border space-y-2 border p-3">
      <label className="flex items-start gap-2">
        <input
          type="checkbox"
          checked={selected}
          disabled={disabled}
          onChange={(event) => onToggle(event.target.checked)}
        />
        <span className="min-w-0 font-mono text-sm">{grant.scope}</span>
      </label>
      <div className="pl-6">
        <Text small muted>
          {appliesToLabel(grant, resourceOptions, serverIndex)}
        </Text>
        <PinnedDimensionChips grant={grant} />
      </div>
      {selected && (
        <div className="grid gap-2 pl-6 sm:grid-cols-2">
          {isMcp && serversFailed && (
            <div className="space-y-1">
              <Text small muted>
                Could not load this organization&rsquo;s MCP servers. Server and
                specific-tool choices are unavailable. Existing policy
                restrictions still apply.
              </Text>
              <Button
                type="button"
                size="sm"
                variant="secondary"
                onClick={onRetryServers}
              >
                Retry servers
              </Button>
            </div>
          )}
          {canNarrowResource(grant) && resourceOptions.length > 0 && (
            <NarrowingSelect
              label={isMcp ? "Server" : "Resource"}
              scope={grant.scope}
              anyLabel={anyResourceLabel(selector.resourceKind)}
              value={narrowing?.resourceId ?? ""}
              options={resourceOptions}
              disabled={disabled}
              // A different server has different tools, so a tool pinned for
              // the previous one would silently stop matching.
              onChange={(value) =>
                update({
                  resourceId: value || undefined,
                  tool: undefined,
                  tools: undefined,
                })
              }
            />
          )}
          {isMcp && (open.has("tool") || open.has("disposition")) && (
            <ToolNarrowing
              entry={entry}
              grant={grant}
              narrowing={narrowing}
              disabled={disabled}
              onChange={update}
            />
          )}
        </div>
      )}
    </div>
  );
}

/**
 * The tool dimension of an mcp grant. Toolset-backed servers enumerate their
 * tools at deploy time; remote-MCP-backed ones only have the metadata the
 * Inspect tab materialized, so they are fetched per server. Anything that
 * cannot be enumerated offers no tool choice at all rather than a free-text
 * name the server would have to reject.
 */
function ToolNarrowing({
  entry,
  grant,
  narrowing,
  disabled,
  onChange,
}: {
  entry: ServerEntry | undefined;
  grant: AgentPolicyGrantForm;
  narrowing: GrantNarrowing;
  disabled?: boolean;
  onChange: (value: GrantNarrowing) => void;
}): JSX.Element {
  const server = entry?.server;
  const remoteBacked = server?.dynamicTools === true && server.remoteBacked;
  const metadata = useToolMetadata(remoteBacked ? server.id : undefined, {
    enabled: remoteBacked,
    projectSlug: entry?.projectSlug,
  });
  const remoteTools = useMemo(
    () =>
      toolMetadataToServerTools(
        server?.id ?? "",
        Object.values(metadata.metadataByTool),
      ),
    [server?.id, metadata.metadataByTool],
  );
  const open = new Set(openDimensions(grant));
  const tools = remoteBacked ? remoteTools : (server?.tools ?? []);
  const panelServers: ToolSelectionServer[] =
    server && open.has("tool")
      ? [
          {
            id: server.id,
            name: server.name,
            tools: tools.map((tool) => {
              const annotations: ToolAnnotation[] = [];
              if (tool.annotations?.readOnlyHint) annotations.push("read_only");
              if (tool.annotations?.destructiveHint)
                annotations.push("destructive");
              if (tool.annotations?.idempotentHint)
                annotations.push("idempotent");
              if (tool.annotations?.openWorldHint)
                annotations.push("open_world");
              return { name: tool.name, annotations };
            }),
            status: remoteBacked
              ? metadata.isLoading
                ? "loading"
                : metadata.isError
                  ? "error"
                  : "ready"
              : server.dynamicTools
                ? "unavailable"
                : "ready",
            unavailableLabel: "Tools are discovered at runtime",
            emptyLabel: "No tools recorded",
            emptyContent:
              "No tools are recorded for this server. You can still restrict tool dispositions.",
            onRetry: metadata.refetch,
          },
        ]
      : [];
  const selectedTools =
    narrowing.tools ?? (narrowing.tool ? [narrowing.tool] : []);
  const selectedAnnotations =
    narrowing.dispositions ??
    (narrowing.disposition ? [narrowing.disposition] : []);
  return (
    <fieldset
      disabled={disabled}
      className="min-w-0 space-y-2 sm:col-span-2"
      aria-label={`Fine-tune ${grant.scope}`}
    >
      <Text small muted>
        Optional: select tool dispositions or specific tools. Existing policy
        restrictions always apply.
      </Text>
      {!server && open.has("tool") && (
        <Text small muted>
          Select a server to choose specific tools.
        </Text>
      )}
      <ToolSelectionPanel
        servers={panelServers}
        mode={
          narrowing.dispositions !== undefined ||
          narrowing.disposition ||
          !open.has("tool") ||
          !server
            ? "annotations"
            : "tools"
        }
        selectedAnnotations={selectedAnnotations}
        selectedTools={selectedTools.map((toolName) => ({
          serverId: server?.id ?? "",
          toolName,
        }))}
        annotationSelectionSupported={open.has("disposition")}
        flattenSingleServer
        toolsTabLabel="Specific tools"
        onSelectionChange={(change) => {
          if (disabled) return;
          onChange({
            tool: undefined,
            disposition: undefined,
            tools:
              change.mode === "tools"
                ? change.tools.map((tool) => tool.toolName)
                : undefined,
            dispositions:
              change.mode === "annotations" ? change.annotations : undefined,
          });
        }}
      />
      {(narrowing.tools !== undefined ||
        narrowing.dispositions !== undefined ||
        narrowing.tool ||
        narrowing.disposition) && (
        <Button
          type="button"
          variant="tertiary"
          size="sm"
          onClick={() =>
            onChange({
              tools: undefined,
              dispositions: undefined,
              tool: undefined,
              disposition: undefined,
            })
          }
        >
          Reset tool restrictions
        </Button>
      )}
    </fieldset>
  );
}

function NarrowingSelect({
  label,
  scope,
  anyLabel,
  value,
  options,
  disabled,
  onChange,
}: {
  label: string;
  scope: string;
  anyLabel: string;
  value: string;
  options: ResourceOption[];
  disabled?: boolean;
  onChange: (value: string) => void;
}): JSX.Element {
  return (
    <label className="space-y-1 text-sm">
      {label}
      <select
        aria-label={`${label} for ${scope}`}
        className={SELECT_CLASS}
        value={value}
        disabled={disabled}
        onChange={(event) => onChange(event.target.value)}
      >
        <option value="">{anyLabel}</option>
        {options.map((option) => (
          <option key={option.id} value={option.id}>
            {option.group ? `${option.group} — ${option.name}` : option.name}
          </option>
        ))}
      </select>
    </label>
  );
}

/** Dimensions the candidate already pins. They are shown, never edited. */
function PinnedDimensionChips({
  grant,
}: {
  grant: AgentPolicyGrantForm;
}): JSX.Element | null {
  const { selector } = grant;
  const chips: string[] = [];
  // Only concrete values are constraints; a wildcard is an open dimension the
  // editor offers instead.
  const pinned = (value: string | undefined) =>
    value !== undefined && value !== ANY_RESOURCE;
  if (pinned(selector.tool)) chips.push(`tool: ${selector.tool}`);
  if (pinned(selector.disposition) && selector.disposition)
    chips.push(DISPOSITION_LABELS[selector.disposition]);
  if (pinned(selector.projectId)) chips.push("one project");
  if (pinned(selector.serverUrl)) chips.push(`server: ${selector.serverUrl}`);
  if (pinned(selector.serverIdentity))
    chips.push(`identity: ${selector.serverIdentity}`);
  if (chips.length === 0) return null;
  return (
    <div className="mt-1 flex flex-wrap items-center gap-1">
      <Text as="span" small muted>
        Restricted to:
      </Text>
      {chips.map((chip) => (
        <Badge key={chip} size="sm">
          {chip}
        </Badge>
      ))}
    </div>
  );
}

function anyResourceLabel(resourceKind: string): string {
  switch (resourceKind) {
    case "mcp":
      return "All MCP servers";
    case "project":
      return "All projects";
    case "skill":
      return "All projects' skills";
    case "environment":
      return "All environments";
    case "risk_policy":
      return "All risk policies";
    default:
      return "All resources";
  }
}

function appliesToLabel(
  grant: AgentPolicyGrantForm,
  resourceOptions: ResourceOption[],
  serverIndex: Map<string, ServerEntry>,
): string {
  const { resourceId, resourceKind } = grant.selector;
  if (resourceId === ANY_RESOURCE) return anyResourceLabel(resourceKind);
  const named =
    serverIndex.get(resourceId)?.server.name ??
    resourceOptions.find((option) => option.id === resourceId)?.name;
  return named ?? resourceId;
}
