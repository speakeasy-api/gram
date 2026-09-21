import { Checkbox } from "@/components/ui/Checkbox";
import { Label } from "@/components/ui/Label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import { Text } from "@/components/ui/Text";
import { useToolMetadata } from "@/hooks/useToolMetadata";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { RiskMCPScope } from "@gram/client/models/components/riskmcpscope.js";
import type { RiskMCPServerScope } from "@gram/client/models/components/riskmcpserverscope.js";
import { useListToolsets } from "@gram/client/react-query/listToolsets.js";
import { useMcpServers } from "@gram/client/react-query/mcpServers.js";
import { useMetaMcpServers } from "@gram/client/react-query/metaMcpServers.js";
import { Loader2, Network, Server } from "lucide-react";
import { useMemo } from "react";

export type PolicyMCPScopeValue = RiskMCPScope | null;

export function PolicyMCPScopePicker({
  value,
  onChange,
}: {
  value: PolicyMCPScopeValue;
  onChange: (value: PolicyMCPScopeValue) => void;
}): JSX.Element {
  const serversQuery = useMcpServers(undefined, undefined, {
    throwOnError: false,
  });
  const gatewaysQuery = useMetaMcpServers(undefined, undefined, {
    throwOnError: false,
  });
  const toolsetsQuery = useListToolsets(undefined, undefined, {
    throwOnError: false,
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
  const selectedByID = new Map(
    (value?.servers ?? []).map((server) => [server.mcpServerId, server]),
  );
  const loading =
    serversQuery.isLoading ||
    gatewaysQuery.isLoading ||
    toolsetsQuery.isLoading;
  const failed =
    serversQuery.isError || gatewaysQuery.isError || toolsetsQuery.isError;

  const setServer = (
    serverID: string,
    selected: boolean,
    tools: string[] = [],
  ) => {
    if (!value) return;
    const servers = value.servers.filter(
      (server) => server.mcpServerId !== serverID,
    );
    if (selected) servers.push({ mcpServerId: serverID, tools });
    servers.sort((a, b) => a.mcpServerId.localeCompare(b.mcpServerId));
    onChange({ servers });
  };

  const updateTools = (serverID: string, tools: string[]) => {
    if (!value) return;
    onChange({
      servers: value.servers.map((server) =>
        server.mcpServerId === serverID ? { ...server, tools } : server,
      ),
    });
  };

  return (
    <div className="space-y-3">
      <div className="space-y-1">
        <Label className="text-sm font-medium">MCP servers</Label>
        <p className="text-muted-foreground text-xs">
          Limit this policy to selected MCP servers, gateways, and tools. Only
          servers you have access to are listed.
        </p>
      </div>
      <RadioGroup
        value={value === null ? "all" : "selected"}
        onValueChange={(mode) =>
          onChange(mode === "all" ? null : { servers: [] })
        }
        className="grid gap-2 sm:grid-cols-2"
      >
        <ScopeMode value="all" label="All MCP servers" />
        <ScopeMode value="selected" label="Selected servers" />
      </RadioGroup>

      {value !== null ? (
        <div className="space-y-2">
          {loading ? (
            <Text small muted className="flex items-center gap-2">
              <Loader2 className="size-4 animate-spin" />
              Loading MCP servers…
            </Text>
          ) : null}
          {failed ? (
            <Text small className="text-destructive">
              Some MCP servers could not be loaded.
            </Text>
          ) : null}
          {(serversQuery.data?.mcpServers ?? []).map((server) => {
            const selected = selectedByID.get(server.id);
            const toolset = server.toolsetId
              ? toolsetsByID.get(server.toolsetId)
              : undefined;
            const tools = toolset
              ? toolset.tools.map((tool) => tool.name)
              : undefined;
            return (
              <ServerScopeRow
                key={server.id}
                server={server}
                selected={selected}
                hostedTools={tools}
                onSelectedChange={(checked) => setServer(server.id, checked)}
                onToolsChange={(next) => updateTools(server.id, next)}
              />
            );
          })}
          {(gatewaysQuery.data?.metaMcpServers ?? []).map((gateway) => {
            const selected = selectedByID.get(gateway.id);
            return (
              <div
                key={gateway.id}
                className="border-border rounded-md border px-3 py-2.5"
              >
                <label className="flex cursor-pointer items-center gap-2">
                  <Checkbox
                    checked={!!selected}
                    onCheckedChange={(checked) =>
                      setServer(gateway.id, checked === true)
                    }
                  />
                  <Network className="text-muted-foreground size-4" />
                  <span className="text-sm font-medium">{gateway.name}</span>
                  <span className="text-muted-foreground ml-auto text-xs">
                    Gateway · all tools
                  </span>
                </label>
              </div>
            );
          })}
          {!loading &&
          (serversQuery.data?.mcpServers?.length ?? 0) === 0 &&
          (gatewaysQuery.data?.metaMcpServers?.length ?? 0) === 0 ? (
            <Text small muted>
              No readable MCP servers are available in this project.
            </Text>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

function ScopeMode({ value, label }: { value: string; label: string }) {
  return (
    <label className="border-border flex cursor-pointer items-center gap-2 rounded-md border px-3 py-2.5">
      <RadioGroupItem value={value} />
      <span className="text-sm font-medium">{label}</span>
    </label>
  );
}

function ServerScopeRow({
  server,
  selected,
  hostedTools,
  onSelectedChange,
  onToolsChange,
}: {
  server: McpServer;
  selected: RiskMCPServerScope | undefined;
  hostedTools: string[] | undefined;
  onSelectedChange: (selected: boolean) => void;
  onToolsChange: (tools: string[]) => void;
}) {
  return (
    <div className="border-border rounded-md border px-3 py-2.5">
      <label className="flex cursor-pointer items-center gap-2">
        <Checkbox
          checked={!!selected}
          onCheckedChange={(checked) => onSelectedChange(checked === true)}
        />
        <Server className="text-muted-foreground size-4" />
        <span className="text-sm font-medium">
          {server.name ?? server.slug ?? "Unnamed MCP server"}
        </span>
      </label>
      {selected ? (
        <ServerTools
          server={server}
          selectedTools={selected.tools ?? []}
          hostedTools={hostedTools}
          onChange={onToolsChange}
        />
      ) : null}
    </div>
  );
}

function ServerTools({
  server,
  selectedTools,
  hostedTools,
  onChange,
}: {
  server: McpServer;
  selectedTools: string[];
  hostedTools: string[] | undefined;
  onChange: (tools: string[]) => void;
}) {
  const metadata = useToolMetadata(server.id, {
    enabled: !!server.remoteMcpServerId && hostedTools === undefined,
  });
  const tools = useMemo(() => {
    const values = hostedTools ?? Object.keys(metadata.metadataByTool);
    return [...new Set(values)].sort((a, b) => a.localeCompare(b));
  }, [hostedTools, metadata.metadataByTool]);
  const allTools = selectedTools.length === 0;

  if (metadata.isLoading) {
    return (
      <Text small muted className="mt-2 flex items-center gap-2 pl-6">
        <Loader2 className="size-3.5 animate-spin" />
        Loading tools…
      </Text>
    );
  }
  if (tools.length === 0) {
    return (
      <Text small muted className="mt-2 pl-6">
        All tools
      </Text>
    );
  }

  return (
    <div className="mt-3 space-y-2 pl-6">
      <RadioGroup
        value={allTools ? "all" : "selected"}
        onValueChange={(mode) => onChange(mode === "all" ? [] : tools)}
        className="flex gap-5"
      >
        <ScopeMode value="all" label="All tools" />
        <ScopeMode value="selected" label="Selected tools" />
      </RadioGroup>
      {!allTools ? (
        <div className="grid gap-2 pt-1 sm:grid-cols-2">
          {tools.map((tool) => {
            const checked = selectedTools.includes(tool);
            return (
              <label key={tool} className="flex items-center gap-2 text-sm">
                <Checkbox
                  checked={checked}
                  onCheckedChange={(nextChecked) => {
                    const next = nextChecked
                      ? [...selectedTools, tool]
                      : selectedTools.filter((name) => name !== tool);
                    if (next.length > 0) onChange([...new Set(next)].sort());
                  }}
                />
                <span className="truncate font-mono text-xs">{tool}</span>
              </label>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}
