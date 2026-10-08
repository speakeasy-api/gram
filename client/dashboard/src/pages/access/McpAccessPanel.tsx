import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { InlineEmptyState } from "@/components/inline-empty-state";
import { useOrganization } from "@/contexts/Auth";
import { useMemo, useState, type JSX } from "react";

import { ForbiddenServers } from "./ForbiddenServers";
import { ToolLimitBadges, ToolLimitMenu } from "./McpAccessParts";
import {
  adminCoverage,
  fuzzyMatch,
  indexServers,
  MCP_CONNECT_SCOPE,
  parseMcpConnectGrant,
  serializeMcpConnectAccess,
  serverHandle,
  type McpConnectAccess,
  type ServerWithProject,
  type ToolLimit,
  type ToolLimitKind,
} from "./mcpAccessModel";
import { McpServerPicker, type ServerPickerActions } from "./McpServerPicker";
import { grantKeysString } from "./roleDialogState";
import {
  ToolAccessSheet,
  type ToolAccessTarget,
  type ToolSheetTab,
} from "./ToolAccessSheet";
import type { RoleGrant } from "./types";
import { useOrgMcpServers } from "./useOrgMcpServers";

function grantKey(grant: RoleGrant | undefined): string {
  return grant ? grantKeysString({ [MCP_CONNECT_SCOPE]: grant }) : "";
}

/**
 * The MCP access tab: which servers a role connects to, which of their tools
 * it can call, and which servers it forbids. Edits the role's `mcp:connect`
 * grant and nothing else.
 */
export function McpAccessPanel({
  grants,
  onChangeConnectGrant,
  onShowPlatformAccess,
}: {
  grants: Record<string, RoleGrant>;
  onChangeConnectGrant: (grant: RoleGrant | undefined) => void;
  onShowPlatformAccess: () => void;
}): JSX.Element {
  const organization = useOrganization();
  const inventory = useOrgMcpServers(true);
  const incoming = grants[MCP_CONNECT_SCOPE];
  const incomingKey = grantKey(incoming);

  // The panel keeps its own reading of the grant, so what saves nothing yet
  // still shows: a server with every tool cleared, or servers set aside while
  // All servers is chosen. A grant changed from elsewhere (the role loading)
  // is read afresh.
  const [state, setState] = useState(() => ({
    access: parseMcpConnectGrant(incoming),
    key: incomingKey,
  }));
  if (state.key !== incomingKey) {
    setState({ access: parseMcpConnectGrant(incoming), key: incomingKey });
  }
  const { access } = state;

  const [query, setQuery] = useState("");
  const [sheet, setSheet] = useState<{
    target: ToolAccessTarget;
    tab: ToolSheetTab;
    apply?: boolean;
  } | null>(null);

  const update = (change: (prev: McpConnectAccess) => McpConnectAccess) => {
    const next = change(access);
    const grant = serializeMcpConnectAccess(next);
    setState({ access: next, key: grantKey(grant) });
    onChangeConnectGrant(grant);
  };

  const setServerLimit = (id: string, limit: ToolLimit | undefined) =>
    update((prev) => {
      const servers = { ...prev.servers };
      if (limit) servers[id] = limit;
      else delete servers[id];
      return { ...prev, servers };
    });

  const coverage = useMemo(
    () => adminCoverage(grants, inventory.groups),
    [grants, inventory.groups],
  );
  const serverIndex = useMemo(
    () => indexServers(inventory.groups),
    [inventory.groups],
  );
  const forbidden = new Set(access.forbidden);
  const pickerGroups = inventory.groups
    .map((group) => ({
      ...group,
      servers: group.servers.filter((s) => !forbidden.has(s.id)),
    }))
    .filter((group) => group.servers.length > 0);
  const defaultProjectId =
    organization.projects.find((p) => p.slug === "default")?.id ??
    pickerGroups[0]?.projectId;

  const openSheet = (entry: ServerWithProject, tab: ToolSheetTab) =>
    setSheet({ target: { kind: "server", entry }, tab });

  const actions: ServerPickerActions = {
    onToggleServer: (entry, checked) =>
      setServerLimit(entry.server.id, checked ? { kind: "all" } : undefined),
    onSetServers: (ids, checked) =>
      update((prev) => {
        const servers = { ...prev.servers };
        for (const id of ids) {
          if (!checked) delete servers[id];
          else servers[id] ??= { kind: "all" };
        }
        return { ...prev, servers };
      }),
    onPickLimit: (entry, kind) => {
      const { server } = entry;
      if (kind === "all") {
        setServerLimit(server.id, { kind: "all" });
        return;
      }
      // The sheet carries access over once the server's tools are known.
      if (!access.servers[server.id]) {
        setServerLimit(server.id, { kind: "all" });
      }
      setSheet({ target: { kind: "server", entry }, tab: kind, apply: true });
    },
    onOpenSheet: (entry) => {
      const limit = access.servers[entry.server.id];
      openSheet(entry, limit?.kind === "annotations" ? "annotations" : "tools");
    },
    onForbid: (id) =>
      update((prev) => {
        const servers = { ...prev.servers };
        delete servers[id];
        return {
          ...prev,
          servers,
          forbidden: prev.forbidden.includes(id)
            ? prev.forbidden
            : [...prev.forbidden, id],
        };
      }),
    onShowPlatformAccess,
  };

  const pickAllServersLimit = (kind: ToolLimitKind) => {
    if (kind === "all") {
      update((prev) => ({ ...prev, allServers: { kind: "all" } }));
      return;
    }
    setSheet({ target: { kind: "all" }, tab: "annotations", apply: true });
  };

  const sheetLimit: ToolLimit = (sheet?.target.kind === "server"
    ? access.servers[sheet.target.entry.server.id]
    : access.allServers) ?? { kind: "all" };

  const forbiddenRows = access.forbidden
    .map((id) => ({ id, entry: serverIndex.get(id) }))
    .filter(
      ({ id, entry }) =>
        // With All servers chosen there is no search, so every block shows.
        !!access.allServers ||
        fuzzyMatch(
          query,
          entry ? `${entry.server.name} ${serverHandle(entry.server)}` : id,
        ),
    );
  const hasHiddenRules =
    access.preservedAllow.length > 0 ||
    access.preservedDeny.length > 0 ||
    access.denyAll;

  return (
    <div className="flex flex-col gap-4 p-4">
      <fieldset className="flex flex-col gap-2">
        <legend className="text-eyebrow mb-2">Server access</legend>
        <RadioCardGroup
          size="sm"
          value={access.allServers ? "all" : "specific"}
          onValueChange={(value) =>
            update((prev) => ({
              ...prev,
              allServers: value === "all" ? { kind: "all" } : null,
            }))
          }
        >
          <RadioCard
            value="specific"
            title={
              <span className="flex flex-wrap items-center gap-2">
                Specific servers
                <span className="text-muted-foreground text-xs font-normal">
                  Recommended
                </span>
              </span>
            }
          >
            Choose servers project by project.
          </RadioCard>
          <RadioCard
            value="all"
            title="All servers"
            detail={
              access.allServers && (
                <span className="flex items-center gap-2">
                  <span className="mr-auto">
                    <ToolLimitBadges
                      limit={access.allServers}
                      onOpen={() =>
                        setSheet({
                          target: { kind: "all" },
                          tab: "annotations",
                        })
                      }
                    />
                  </span>
                  <ToolLimitMenu
                    label="More options for all servers"
                    current={access.allServers.kind}
                    offerByTool={false}
                    onPick={pickAllServersLimit}
                  />
                </span>
              )
            }
          >
            Every server in every project, including ones added later.
          </RadioCard>
        </RadioCardGroup>
      </fieldset>

      {hasHiddenRules && (
        <Alert variant="default" alignTop className="text-sm">
          This role also has MCP access rules this view can&rsquo;t show, such
          as access to a whole project. They stay as they are when you save.
        </Alert>
      )}

      {inventory.isError && (
        <Alert variant="error" alignTop className="text-sm">
          <span className="flex flex-wrap items-center gap-2">
            Could not load this organization&rsquo;s MCP servers.
            <Button variant="tertiary" size="xs" onClick={inventory.refetch}>
              <Button.Text>Retry</Button.Text>
            </Button>
          </span>
        </Alert>
      )}

      {!access.allServers && (
        <ServerPickerArea
          settled={inventory.settled}
          isError={inventory.isError}
          isEmpty={inventory.groups.length === 0}
        >
          <McpServerPicker
            groups={pickerGroups}
            access={access}
            coverage={coverage}
            query={query}
            onQueryChange={setQuery}
            defaultProjectId={defaultProjectId}
            actions={actions}
          />
        </ServerPickerArea>
      )}

      <ForbiddenServers
        servers={forbiddenRows}
        onUnblock={(id) =>
          update((prev) => ({
            ...prev,
            forbidden: prev.forbidden.filter((other) => other !== id),
          }))
        }
      />

      <ToolAccessSheet
        target={sheet?.target ?? null}
        limit={sheetLimit}
        initialTab={sheet?.tab ?? "tools"}
        applyTab={sheet?.apply}
        onChange={(limit) => {
          if (!sheet) return;
          if (sheet.target.kind === "all") {
            update((prev) => ({ ...prev, allServers: limit }));
          } else {
            setServerLimit(sheet.target.entry.server.id, limit);
          }
        }}
        onClose={() => setSheet(null)}
      />
    </div>
  );
}

/** The picker once the inventory is in; what is happening until then. */
function ServerPickerArea({
  settled,
  isError,
  isEmpty,
  children,
}: {
  settled: boolean;
  isError: boolean;
  isEmpty: boolean;
  children: React.ReactNode;
}): JSX.Element | null {
  if (isError) return null;
  if (!settled) {
    return (
      <p className="text-muted-foreground py-6 text-center text-sm">
        Loading servers…
      </p>
    );
  }
  if (isEmpty) {
    return (
      <InlineEmptyState
        icon="network"
        heading="No MCP servers yet"
        description="Servers added to any project in this organization show up here."
      />
    );
  }
  return <>{children}</>;
}
