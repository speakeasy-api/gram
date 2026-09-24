import { Button } from "@/components/ui/Button";
import { Checkbox } from "@/components/ui/Checkbox";
import { Input } from "@/components/ui/Input";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import { SegmentedControl } from "@/components/ui/SegmentedControl";
import { Text } from "@/components/ui/Text";
import { useProjectSlugForRequests, useSdkClient } from "@/contexts/Sdk";
import { cn } from "@/lib/utils";
import type {
  RiskMCPScope,
  RiskMCPScopeToolAnnotations,
} from "@gram/client/models/components/riskmcpscope.js";
import type { RiskMCPServerScope } from "@gram/client/models/components/riskmcpserverscope.js";
import { useListToolsets } from "@gram/client/react-query/listToolsets.js";
import { buildListMcpServerToolMetadataQuery } from "@gram/client/react-query/listMcpServerToolMetadata.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useMetaMcpMembers } from "@gram/client/react-query/metaMcpMembers.js";
import { useMetaMcpServers } from "@gram/client/react-query/metaMcpServers.js";
import { useQueries } from "@tanstack/react-query";
import { ChevronDown, Info, Loader2, Network, Server, X } from "lucide-react";
import { useMemo, useState } from "react";

export type PolicyScopeMode = "everywhere" | "mcp";
export type ToolAnnotation = RiskMCPScopeToolAnnotations;

export interface PolicyMCPScopeValue {
  mode: PolicyScopeMode;
  allServers: boolean;
  toolAnnotations: ToolAnnotation[];
  servers: RiskMCPServerScope[];
}

export function policyMCPScopeValue(
  scope: RiskMCPScope | null | undefined,
): PolicyMCPScopeValue {
  return {
    mode: scope ? "mcp" : "everywhere",
    allServers: scope?.allServers ?? false,
    toolAnnotations: [...(scope?.toolAnnotations ?? [])],
    servers:
      scope?.servers.map((server) => ({
        mcpServerId: server.mcpServerId,
        ...(server.tools === undefined ? {} : { tools: [...server.tools] }),
      })) ?? [],
  };
}

export function policyMCPScopePayload(
  value: PolicyMCPScopeValue,
): RiskMCPScope | null {
  if (value.mode === "everywhere") return null;
  return {
    allServers: value.allServers,
    toolAnnotations: [...value.toolAnnotations].sort(),
    servers: value.servers
      .map((server) => ({
        mcpServerId: server.mcpServerId,
        ...(server.tools === undefined
          ? {}
          : { tools: [...server.tools].sort() }),
      }))
      .sort((left, right) => left.mcpServerId.localeCompare(right.mcpServerId)),
  };
}

type AnnotationFields = Partial<Record<ToolAnnotation, boolean>>;

type PickerTool = {
  name: string;
  annotations: ToolAnnotation[];
};

type PickerServer = {
  id: string;
  name: string;
  slug?: string;
  kind: "server" | "gateway";
  memberCount?: number;
  tools: PickerTool[];
  toolsLoading: boolean;
};

type ServerSelection =
  | { kind: "off" }
  | { kind: "rule"; derived: boolean }
  | { kind: "custom"; tools: string[] };

const TOOL_ANNOTATIONS: Array<{
  value: ToolAnnotation;
  label: string;
}> = [
  { value: "destructiveHint", label: "Destructive" },
  { value: "readOnlyHint", label: "Read-only" },
  { value: "idempotentHint", label: "Idempotent" },
  { value: "openWorldHint", label: "Open world" },
];

function annotationNames(
  value: AnnotationFields | undefined,
): ToolAnnotation[] {
  if (!value) return [];
  return TOOL_ANNOTATIONS.filter(({ value: annotation }) => value[annotation])
    .map(({ value: annotation }) => annotation)
    .sort();
}

export function PolicyMCPScopePicker({
  value,
  onChange,
  action,
}: {
  value: PolicyMCPScopeValue;
  onChange: (value: PolicyMCPScopeValue) => void;
  action: string;
}): JSX.Element {
  const gramProject = useProjectSlugForRequests();
  const client = useSdkClient();
  const serversQuery = useMcpServers({ gramProject }, undefined, {
    throwOnError: false,
  });
  const gatewaysQuery = useMetaMcpServers({ gramProject }, undefined, {
    throwOnError: false,
  });
  const toolsetsQuery = useListToolsets({ gramProject }, undefined, {
    throwOnError: false,
  });
  const concreteServers = serversQuery.data?.mcpServers ?? [];
  const remoteServers = concreteServers.filter(
    (server) => !server.toolsetId && !!server.remoteMcpServerId,
  );
  const metadataQueries = useQueries({
    queries: remoteServers.map((server) => ({
      ...buildListMcpServerToolMetadataQuery(client, {
        mcpServerId: server.id,
        gramProject,
      }),
      throwOnError: false,
    })),
  });

  const toolsetsByID = useMemo(
    () =>
      new Map(
        (toolsetsQuery.data?.toolsets ?? []).map((toolset) => [
          toolset.id,
          toolset,
        ]),
      ),
    [toolsetsQuery.data?.toolsets],
  );
  const metadataByServerID = new Map(
    remoteServers.map((server, index) => [server.id, metadataQueries[index]]),
  );
  const pickerServers: PickerServer[] = [
    ...concreteServers.map((server): PickerServer => {
      const toolset = server.toolsetId
        ? toolsetsByID.get(server.toolsetId)
        : undefined;
      const metadataQuery = metadataByServerID.get(server.id);
      const tools = toolset
        ? toolset.tools.map((tool) => ({
            name: tool.name,
            annotations: annotationNames(
              tool.annotations as AnnotationFields | undefined,
            ),
          }))
        : (metadataQuery?.data?.tools ?? []).map((tool) => ({
            name: tool.toolName,
            annotations: annotationNames(tool),
          }));
      tools.sort((left, right) => left.name.localeCompare(right.name));
      return {
        id: server.id,
        name: server.name ?? server.slug ?? "Unnamed MCP server",
        slug: server.slug,
        kind: "server",
        tools,
        toolsLoading: metadataQuery?.isLoading ?? false,
      };
    }),
    ...(gatewaysQuery.data?.metaMcpServers ?? []).map(
      (gateway): PickerServer => ({
        id: gateway.id,
        name: gateway.name,
        kind: "gateway",
        memberCount: gateway.memberCount,
        tools: [],
        toolsLoading: false,
      }),
    ),
  ];
  const [focusedServerID, setFocusedServerID] = useState("");
  const [search, setSearch] = useState("");
  const [ruleOpen, setRuleOpen] = useState(false);
  const storedByID = new Map(
    value.servers.map((server) => [server.mcpServerId, server]),
  );
  const firstSelected = pickerServers.find((server) => {
    if (storedByID.has(server.id)) return true;
    return value.allServers && server.kind === "server";
  });
  const focusedServer =
    pickerServers.find((server) => server.id === focusedServerID) ??
    firstSelected ??
    pickerServers[0];
  const gatewayMembersQuery = useMetaMcpMembers(
    { metaMcpServerId: focusedServer?.id ?? "", gramProject },
    undefined,
    {
      enabled: focusedServer?.kind === "gateway",
      throwOnError: false,
    },
  );
  const filteredServers = pickerServers.filter((server) =>
    `${server.name} ${server.slug ?? ""}`
      .toLocaleLowerCase()
      .includes(search.trim().toLocaleLowerCase()),
  );
  const loading =
    serversQuery.isLoading ||
    gatewaysQuery.isLoading ||
    toolsetsQuery.isLoading;
  const failed =
    serversQuery.isError || gatewaysQuery.isError || toolsetsQuery.isError;

  const selectionFor = (server: PickerServer): ServerSelection => {
    const stored = storedByID.get(server.id);
    if (stored?.tools !== undefined) {
      return { kind: "custom", tools: stored.tools };
    }
    if (stored) return { kind: "rule", derived: false };
    if (value.allServers && server.kind === "server") {
      return { kind: "rule", derived: true };
    }
    return { kind: "off" };
  };
  const matchesRule = (tool: PickerTool) =>
    value.toolAnnotations.length === 0 ||
    tool.annotations.some((annotation) =>
      value.toolAnnotations.includes(annotation),
    );
  const ruleTools = (server: PickerServer) =>
    server.tools.filter(matchesRule).map((tool) => tool.name);
  const replaceServer = (serverID: string, next: RiskMCPServerScope | null) => {
    const servers = value.servers.filter(
      (server) => server.mcpServerId !== serverID,
    );
    if (next) servers.push(next);
    onChange({ ...value, servers });
  };
  const removeServer = (server: PickerServer) => {
    if (value.allServers && server.kind === "server") {
      const customByID = new Map(
        value.servers
          .filter((entry) => entry.tools !== undefined)
          .map((entry) => [entry.mcpServerId, entry]),
      );
      const gateways = value.servers.filter((entry) =>
        pickerServers.some(
          (candidate) =>
            candidate.id === entry.mcpServerId && candidate.kind === "gateway",
        ),
      );
      const materialized = concreteServers
        .filter((candidate) => candidate.id !== server.id)
        .map(
          (candidate) =>
            customByID.get(candidate.id) ?? { mcpServerId: candidate.id },
        );
      onChange({
        ...value,
        allServers: false,
        servers: [...materialized, ...gateways],
      });
      return;
    }
    replaceServer(server.id, null);
  };
  const selectRule = (server: PickerServer) => {
    if (value.allServers && server.kind === "server") {
      replaceServer(server.id, null);
      return;
    }
    replaceServer(server.id, { mcpServerId: server.id });
  };
  const toggleServer = (server: PickerServer) => {
    const selection = selectionFor(server);
    if (selection.kind === "off") selectRule(server);
    else removeServer(server);
  };
  const setAllServers = (checked: boolean) => {
    if (checked) {
      const retained = value.servers.filter((entry) => {
        const server = pickerServers.find(
          (candidate) => candidate.id === entry.mcpServerId,
        );
        return server?.kind === "gateway" || entry.tools !== undefined;
      });
      onChange({ ...value, allServers: true, servers: retained });
      return;
    }
    const existingByID = new Map(
      value.servers.map((entry) => [entry.mcpServerId, entry]),
    );
    const materialized = concreteServers.map(
      (server) => existingByID.get(server.id) ?? { mcpServerId: server.id },
    );
    const gateways = value.servers.filter((entry) =>
      pickerServers.some(
        (server) =>
          server.id === entry.mcpServerId && server.kind === "gateway",
      ),
    );
    onChange({
      ...value,
      allServers: false,
      servers: [...materialized, ...gateways],
    });
  };
  const toggleTool = (server: PickerServer, toolName: string) => {
    const selection = selectionFor(server);
    if (selection.kind === "off") return;
    const ruleSelection = ruleTools(server);
    const current =
      selection.kind === "custom" ? selection.tools : ruleSelection;
    const next = current.includes(toolName)
      ? current.filter((name) => name !== toolName)
      : [...current, toolName];
    next.sort();
    if (next.length === 0) {
      removeServer(server);
    } else if (
      next.length === ruleSelection.length &&
      next.every((name, index) => name === ruleSelection[index])
    ) {
      selectRule(server);
    } else {
      replaceServer(server.id, { mcpServerId: server.id, tools: next });
    }
  };

  const explicitCount = pickerServers.filter(
    (server) => selectionFor(server).kind !== "off",
  ).length;
  const toolsInScope = pickerServers.reduce((total, server) => {
    const selection = selectionFor(server);
    if (selection.kind === "custom") return total + selection.tools.length;
    if (selection.kind === "rule") return total + ruleTools(server).length;
    return total;
  }, 0);
  const ruleLabel =
    value.toolAnnotations.length === 0
      ? "All tools"
      : value.toolAnnotations.length === 1
        ? `${TOOL_ANNOTATIONS.find(({ value: annotation }) => annotation === value.toolAnnotations[0])?.label ?? value.toolAnnotations[0]} tools`
        : `${value.toolAnnotations.length} annotations`;

  return (
    <div className="space-y-6">
      <div className="flex flex-nowrap items-center justify-between gap-4">
        <Text small muted className="min-w-0 flex-1 text-pretty">
          {value.mode === "everywhere"
            ? "Every chat session and MCP tool call in this project."
            : "Only tool calls through the servers below, checked at the gateway before the tool runs."}
        </Text>
        <SegmentedControl
          value={value.mode}
          onChange={(mode) => onChange({ ...value, mode })}
          options={[
            { value: "everywhere", label: "Everywhere" },
            { value: "mcp", label: "Selected MCP servers" },
          ]}
          className="h-9"
        />
      </div>

      <div
        className={cn(
          "grid transition-[grid-template-rows,opacity,margin] duration-[260ms] ease-[cubic-bezier(0.215,0.61,0.355,1)]",
          value.mode === "mcp"
            ? "grid-rows-[1fr] opacity-100"
            : "-mt-3 grid-rows-[0fr] opacity-0",
        )}
      >
        <div
          className={cn(
            "min-h-0",
            ruleOpen ? "overflow-visible" : "overflow-hidden",
          )}
        >
          <div className="border-border border">
            <div className="bg-muted/30 border-border grid grid-cols-[minmax(220px,300px)_minmax(0,1fr)] border-b">
              <div className="border-border flex items-center justify-between border-r px-3 py-2">
                <span className="text-muted-foreground font-mono text-[11px] tracking-[0.1em] uppercase">
                  Servers
                </span>
                <span className="font-mono text-xs">
                  {value.allServers ? "All" : `${explicitCount} selected`}
                </span>
              </div>
              <div className="flex items-center gap-3 px-3 py-2">
                <span className="text-muted-foreground font-mono text-[11px] tracking-[0.1em] uppercase">
                  Tools per server
                </span>
                <span className="text-muted-foreground ml-auto text-xs">
                  Default for selected servers
                </span>
                <Popover open={ruleOpen} onOpenChange={setRuleOpen}>
                  <PopoverTrigger asChild>
                    <button
                      type="button"
                      aria-label={`Tool rule: ${ruleLabel}`}
                      className="border-input bg-card flex h-8 w-[220px] items-center gap-2 border px-3 text-left text-sm"
                    >
                      <span className="min-w-0 flex-1 truncate">
                        {ruleLabel}
                      </span>
                      <ChevronDown className="text-muted-foreground size-3.5 shrink-0" />
                    </button>
                  </PopoverTrigger>
                  <PopoverContent
                    align="end"
                    sideOffset={12}
                    className="border-border w-[340px] p-0 shadow-[0_8px_24px_rgba(0,0,0,0.08)]"
                  >
                    <RadioGroup
                      value={
                        value.toolAnnotations.length === 0
                          ? "all"
                          : "annotations"
                      }
                      onValueChange={(mode) =>
                        onChange({
                          ...value,
                          toolAnnotations:
                            mode === "all" ? [] : ["destructiveHint"],
                        })
                      }
                    >
                      <label className="border-border flex cursor-pointer items-start gap-3 border-b p-3">
                        <RadioGroupItem value="all" className="mt-0.5" />
                        <span>
                          <span className="block text-sm font-medium">
                            All tools
                          </span>
                          <span className="text-muted-foreground block text-xs">
                            Every tool, including ones added later
                          </span>
                        </span>
                      </label>
                      <label className="flex cursor-pointer items-start gap-3 p-3 pb-2">
                        <RadioGroupItem
                          value="annotations"
                          className="mt-0.5"
                        />
                        <span>
                          <span className="block text-sm font-medium">
                            Tools with MCP annotations
                          </span>
                          <span className="text-muted-foreground block text-xs">
                            Matches any checked hint. New tools are included
                            when they match.
                          </span>
                        </span>
                      </label>
                    </RadioGroup>
                    <div
                      className={cn(
                        "space-y-2 px-4 pb-3 pl-10",
                        value.toolAnnotations.length === 0 && "opacity-50",
                      )}
                    >
                      {TOOL_ANNOTATIONS.map((annotation) => (
                        <label
                          key={annotation.value}
                          className="flex items-center gap-2 text-sm"
                        >
                          <Checkbox
                            checked={value.toolAnnotations.includes(
                              annotation.value,
                            )}
                            disabled={value.toolAnnotations.length === 0}
                            onCheckedChange={(checked) => {
                              const next = checked
                                ? [...value.toolAnnotations, annotation.value]
                                : value.toolAnnotations.filter(
                                    (value) => value !== annotation.value,
                                  );
                              onChange({
                                ...value,
                                toolAnnotations: [...new Set(next)].sort(),
                              });
                            }}
                          />
                          <span>{annotation.label}</span>
                          <code className="text-muted-foreground/50 ml-auto font-mono text-[11px]">
                            {annotation.value}
                          </code>
                        </label>
                      ))}
                    </div>
                    <div className="border-border flex items-center justify-between border-t px-3 py-2">
                      <Text small muted>
                        Tools without annotations never match.
                      </Text>
                      <Button
                        variant="secondary"
                        size="xs"
                        onClick={() => setRuleOpen(false)}
                      >
                        <Button.Text>Done</Button.Text>
                      </Button>
                    </div>
                  </PopoverContent>
                </Popover>
              </div>
            </div>

            <div className="grid min-h-[420px] grid-cols-[minmax(220px,300px)_minmax(0,1fr)]">
              <div className="border-border border-r">
                <div className="border-border border-b p-2">
                  <Input
                    aria-label="Search MCP servers"
                    placeholder="Search servers"
                    value={search}
                    onChange={setSearch}
                    className="h-8"
                  />
                </div>
                <label className="border-border flex cursor-pointer items-center gap-3 border-b px-3 py-2.5">
                  <Checkbox
                    aria-label="All MCP servers"
                    checked={
                      value.allServers
                        ? true
                        : explicitCount > 0
                          ? "indeterminate"
                          : false
                    }
                    onCheckedChange={(checked) =>
                      setAllServers(checked === true)
                    }
                  />
                  <span className="min-w-0">
                    <span className="block text-sm font-medium">
                      All MCP servers
                    </span>
                    <span className="text-muted-foreground block text-xs">
                      Including servers added later
                    </span>
                  </span>
                </label>
                {loading ? (
                  <Text small muted className="flex items-center gap-2 p-3">
                    <Loader2 className="size-4 animate-spin" />
                    Loading MCP servers...
                  </Text>
                ) : null}
                {failed ? (
                  <Text small className="text-destructive p-3">
                    Some MCP servers could not be loaded.
                  </Text>
                ) : null}
                {filteredServers.map((server) => {
                  const selection = selectionFor(server);
                  const focused = focusedServer?.id === server.id;
                  const count =
                    server.kind === "gateway"
                      ? selection.kind === "rule" &&
                        value.toolAnnotations.length > 0
                        ? "RULE"
                        : selection.kind === "off"
                          ? "0"
                          : "ALL"
                      : selection.kind === "off"
                        ? `0/${server.tools.length}`
                        : selection.kind === "custom"
                          ? `${selection.tools.length}/${server.tools.length}`
                          : value.toolAnnotations.length === 0
                            ? "ALL"
                            : `${ruleTools(server).length}/${server.tools.length}`;
                  return (
                    <div
                      key={server.id}
                      role="button"
                      tabIndex={0}
                      onClick={() => setFocusedServerID(server.id)}
                      onKeyDown={(event) => {
                        if (event.key === "Enter" || event.key === " ") {
                          setFocusedServerID(server.id);
                        }
                      }}
                      className={cn(
                        "border-muted flex cursor-pointer items-center gap-2 border-b px-3 py-2.5",
                        focused &&
                          "bg-muted/55 shadow-[inset_2px_0_0_0_var(--foreground)]",
                      )}
                    >
                      <Checkbox
                        aria-label={server.name}
                        checked={
                          selection.kind === "custom" &&
                          selection.tools.length < server.tools.length
                            ? "indeterminate"
                            : selection.kind !== "off"
                        }
                        className={cn(
                          selection.kind === "rule" &&
                            selection.derived &&
                            "data-[state=checked]:border-muted-foreground data-[state=checked]:bg-muted-foreground data-[state=indeterminate]:border-muted-foreground data-[state=indeterminate]:bg-muted-foreground",
                        )}
                        onClick={(event) => event.stopPropagation()}
                        onCheckedChange={() => toggleServer(server)}
                      />
                      {server.kind === "gateway" ? (
                        <Network className="text-muted-foreground size-4" />
                      ) : (
                        <Server className="text-muted-foreground size-4" />
                      )}
                      <span className="min-w-0 flex-1 truncate text-sm">
                        {server.name}
                      </span>
                      <span className="text-muted-foreground font-mono text-xs">
                        {count}
                      </span>
                    </div>
                  );
                })}
                {!loading && pickerServers.length === 0 ? (
                  <Text small muted className="p-3">
                    No readable MCP servers are available in this project.
                  </Text>
                ) : null}
              </div>

              <div className="min-w-0 p-5">
                {focusedServer ? (
                  <FocusedServerPane
                    server={focusedServer}
                    selection={selectionFor(focusedServer)}
                    ruleTools={ruleTools(focusedServer)}
                    toolAnnotations={value.toolAnnotations}
                    gatewayMembers={
                      gatewayMembersQuery.data?.members?.map((member) => ({
                        id: member.mcpServerId,
                        name:
                          member.mcpServerName ??
                          member.mcpServerSlug ??
                          member.mcpServerId,
                      })) ?? []
                    }
                    gatewayMembersLoading={gatewayMembersQuery.isLoading}
                    onToggleServer={() => toggleServer(focusedServer)}
                    onRemove={() => removeServer(focusedServer)}
                    onUseRule={() => selectRule(focusedServer)}
                    onToggleTool={(toolName) =>
                      toggleTool(focusedServer, toolName)
                    }
                  />
                ) : (
                  <div className="border-border flex min-h-40 items-center justify-center border border-dashed">
                    <Text small muted>
                      Select a server to inspect its tools.
                    </Text>
                  </div>
                )}
              </div>
            </div>

            <div className="bg-muted/30 border-border flex min-h-10 items-center gap-4 border-t px-4 py-2.5">
              <span className="font-mono text-xs">
                {explicitCount} servers · {toolsInScope} tools in scope
              </span>
              {action === "block" ? (
                <span className="text-muted-foreground ml-auto flex items-center gap-1.5 text-xs">
                  <Info className="size-3.5" />
                  Block runs before each matching call. Detector time adds to
                  call latency.
                </span>
              ) : null}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

function FocusedServerPane({
  server,
  selection,
  ruleTools,
  toolAnnotations,
  gatewayMembers,
  gatewayMembersLoading,
  onToggleServer,
  onRemove,
  onUseRule,
  onToggleTool,
}: {
  server: PickerServer;
  selection: ServerSelection;
  ruleTools: string[];
  toolAnnotations: ToolAnnotation[];
  gatewayMembers: Array<{ id: string; name: string }>;
  gatewayMembersLoading: boolean;
  onToggleServer: () => void;
  onRemove: () => void;
  onUseRule: () => void;
  onToggleTool: (toolName: string) => void;
}): JSX.Element {
  const selected = selection.kind !== "off";
  const custom = selection.kind === "custom";
  const selectedTools = custom ? selection.tools : ruleTools;
  const followingAnnotationRule =
    selection.kind === "rule" && toolAnnotations.length > 0;
  const note = !selected
    ? "Not in scope"
    : custom
      ? `Custom · ${selectedTools.length} of ${server.tools.length}`
      : toolAnnotations.length === 0
        ? "All tools, including ones added later"
        : `Following the tool rule · ${selectedTools.length} of ${server.tools.length}`;

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <h3 className="truncate text-base font-medium">{server.name}</h3>
          {server.slug ? (
            <p className="text-muted-foreground truncate font-mono text-xs">
              {server.slug}
            </p>
          ) : null}
          <p className="text-muted-foreground text-xs">
            {server.kind === "gateway"
              ? `${server.memberCount ?? gatewayMembers.length} member servers`
              : `${server.tools.length} tools`}
          </p>
        </div>
        {selected ? (
          <button
            type="button"
            onClick={onRemove}
            className="text-muted-foreground hover:text-foreground flex shrink-0 items-center gap-1 font-mono text-xs"
          >
            <X className="size-3.5" /> Remove from scope
          </button>
        ) : null}
      </div>

      {server.kind === "gateway" ? (
        selected ? (
          <div className="space-y-3">
            <Text small muted>
              A gateway scope covers every tool on its member servers.
              Membership resolves when each call is made, so later membership
              changes apply automatically.
            </Text>
            <div className="border-border border">
              {gatewayMembersLoading ? (
                <Text small muted className="flex items-center gap-2 p-3">
                  <Loader2 className="size-4 animate-spin" /> Loading members...
                </Text>
              ) : gatewayMembers.length > 0 ? (
                gatewayMembers.map((member) => (
                  <div
                    key={member.id}
                    className="border-muted flex items-center gap-2 border-b px-3 py-2 last:border-b-0"
                  >
                    <Server className="text-muted-foreground size-4" />
                    <span className="text-sm">{member.name}</span>
                  </div>
                ))
              ) : (
                <Text small muted className="p-3">
                  No member servers.
                </Text>
              )}
            </div>
          </div>
        ) : (
          <div className="border-border flex min-h-40 flex-col items-center justify-center gap-3 border border-dashed p-6 text-center">
            <Text small muted>
              Calls through {server.name} are not evaluated by this policy.
            </Text>
            <Button variant="secondary" size="sm" onClick={onToggleServer}>
              <Button.Text>Add to scope</Button.Text>
            </Button>
          </div>
        )
      ) : (
        <div className="border-muted border">
          <div className="bg-muted/30 border-muted flex flex-wrap items-center gap-2 border-b px-3 py-2">
            <Checkbox
              aria-label={`Tools on ${server.name}`}
              checked={
                custom && selectedTools.length < server.tools.length
                  ? "indeterminate"
                  : selected
              }
              onCheckedChange={onToggleServer}
            />
            <span className="text-sm font-medium whitespace-nowrap">
              Tools on {server.name}
            </span>
            <span className="text-muted-foreground ml-auto text-xs">
              {note}
            </span>
            {custom ? (
              <button
                type="button"
                onClick={onUseRule}
                className="text-muted-foreground hover:text-foreground text-xs underline"
              >
                Use tool rule
              </button>
            ) : null}
          </div>
          {server.toolsLoading ? (
            <Text small muted className="flex items-center gap-2 p-3">
              <Loader2 className="size-4 animate-spin" /> Loading tools...
            </Text>
          ) : server.tools.length === 0 ? (
            <Text small muted className="p-3">
              No tools are available for this server.
            </Text>
          ) : (
            server.tools.map((tool) => {
              const checked = selected && selectedTools.includes(tool.name);
              const matchedLabels = TOOL_ANNOTATIONS.filter(
                ({ value }) =>
                  toolAnnotations.includes(value) &&
                  tool.annotations.includes(value),
              ).map(({ label }) => label);
              return (
                <label
                  key={tool.name}
                  className="border-muted flex items-center gap-3 border-b px-3 py-2 last:border-b-0"
                >
                  <Checkbox
                    aria-label={tool.name}
                    checked={checked}
                    disabled={!selected}
                    className={cn(
                      !custom &&
                        checked &&
                        "data-[state=checked]:border-muted-foreground data-[state=checked]:bg-muted-foreground",
                    )}
                    onCheckedChange={() => onToggleTool(tool.name)}
                  />
                  <code className="min-w-0 flex-1 truncate font-mono text-xs">
                    {tool.name}
                  </code>
                  {followingAnnotationRule && matchedLabels.length > 0 ? (
                    <span className="text-muted-foreground text-[11px]">
                      {matchedLabels.join(", ")}
                    </span>
                  ) : null}
                </label>
              );
            })
          )}
        </div>
      )}
    </div>
  );
}
