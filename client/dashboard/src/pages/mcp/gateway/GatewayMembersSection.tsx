import { Page } from "@/components/page-layout";
import { SourceMcpIcon } from "@/components/sources/SourceCard";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { useRBAC } from "@/hooks/useRBAC";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import {
  ServerPickerSheet,
  type ServerPickerOption,
} from "@/components/mcp/ServerPickerSheet";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Table, type Column } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { useSdkClient } from "@/contexts/Sdk";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import { useNavigate } from "react-router";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import type { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { invalidateAllMetaMcpMembers } from "@gram/client/react-query/metaMcpMembers.js";

import { useQueryClient } from "@tanstack/react-query";
import {
  ArrowDown,
  ArrowUp,
  Blocks,
  Boxes,
  Cable,
  ChevronDown,
  Cloud,
  Code,
  FileCode,
  Globe,
  Loader2,
  Plus,
  Server,
  Trash2,
} from "lucide-react";
import { Fragment, useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import {
  type MemberClassification,
  type MemberRow,
  buildAddCandidates,
  classifyMemberServer,
  addCandidateBatch,
  reconcileCompletedMembers,
  candidateKey,
  candidateName,
  type AddCandidate,
  type AddResult,
  type AddBatchState,
  memberBackendKind,
  nextSortOrder,
  planReorder,
} from "./memberRows";
import {
  useGatewayMemberRows,
  useReconcileWrappers,
} from "./useGatewayMemberRows";
import { useToolsets } from "../../toolsets/useToolsets";
import { memberWrappers } from "./ensureToolsetWrapper";
import { BuiltinGatewayMembersSection } from "./BuiltinGatewayMembersSection";

const CLASSIFICATION_LABEL: Record<MemberClassification, string> = {
  builtin: "Built-in",
  hosted: "Hosted",
  proxied: "Proxied",
  disabled: "Disabled",
  unproxied: "Unproxied",
  slugless: "No slug",
  unknown: "Unknown",
};

// The gateway's whole point is heterogeneous members behind one URL, so the
// backend column names each member's actual kind rather than a flat
// hosted/proxied split.
const BACKEND_KIND_PRESENTATION = {
  hosted: { label: "Hosted", Icon: Server },
  remote: { label: "Remote", Icon: Globe },
  tunneled: { label: "Tunneled", Icon: Cable },
} as const;

function BackendKindTag({ row }: { row: MemberRow }): JSX.Element {
  const kind = memberBackendKind(row.server);
  if (!kind) {
    return (
      <Text muted small>
        {CLASSIFICATION_LABEL[row.classification]}
      </Text>
    );
  }
  const { label, Icon } = BACKEND_KIND_PRESENTATION[kind];
  return (
    <span className="text-muted-foreground inline-flex items-center gap-1.5 font-mono text-xs tracking-wide uppercase">
      <Icon className="size-3.5" aria-hidden />
      {label}
    </span>
  );
}

// Status is backend-attested only, and each state says why: a badge reading
// "Unknown" with no explanation reads as broken rather than as work the
// runtime hasn't reached yet.
const STATUS_BY_CLASSIFICATION: Record<
  MemberClassification,
  { label: string; variant: "success" | "neutral" | "warning"; why: string }
> = {
  builtin: {
    label: "Restricted",
    variant: "neutral",
    why: "Requires an explicit machine connection grant and valid project credentials. Human connections are blocked.",
  },
  hosted: {
    label: "Available",
    variant: "success",
    why: "Toolset-backed, so the gateway executes its tools in-process.",
  },
  proxied: {
    label: "Unknown",
    variant: "neutral",
    why: "The gateway can't reach this member's upstream yet, so it reports no health. Drill-down and execution arrive with per-upstream credential routing.",
  },
  disabled: {
    label: "Excluded",
    variant: "warning",
    why: "The backing server is disabled, so the gateway serves nothing for it. Re-enable it on the server's own page.",
  },
  unproxied: {
    label: "Excluded",
    variant: "warning",
    why: "Unproxied servers are connected to directly with the vendor's own credentials, so a gateway has nothing to route.",
  },
  slugless: {
    label: "Excluded",
    variant: "warning",
    why: "Without a slug there is no qualified name (server--tool) to address this member by.",
  },
  unknown: {
    label: "Unknown",
    variant: "neutral",
    why: "This member's backing server couldn't be resolved.",
  },
};

// A filled green dot for attested health, a hollow ring for "unobserved",
// amber for excluded: quieter than boxed uppercase badges, and "Unknown"
// stops reading as broken.
const STATUS_DOT_CLASS: Record<"success" | "neutral" | "warning", string> = {
  success: "bg-emerald-500",
  neutral: "border-muted-foreground/60 border bg-transparent",
  warning: "bg-amber-500",
};

function MemberStatusBadge({
  classification,
}: {
  classification: MemberClassification;
}): JSX.Element {
  const status = STATUS_BY_CLASSIFICATION[classification];
  return (
    <SimpleTooltip tooltip={status.why}>
      <span className="inline-flex cursor-default items-center gap-2">
        <span
          className={`size-2 shrink-0 rounded-full ${STATUS_DOT_CLASS[status.variant]}`}
          aria-hidden
        />
        <Text as="span" muted small>
          {status.label}
        </Text>
      </span>
    </SimpleTooltip>
  );
}

function MemberNameCell({ row }: { row: MemberRow }): JSX.Element {
  const routes = useRoutes();
  const name = row.server?.name || row.member.mcpServerName || "MCP server";
  const slug = row.server?.slug ?? row.member.mcpServerSlug;
  return (
    <div className="flex min-w-0 items-center gap-2.5">
      {row.member.builtinKey ? (
        <Blocks className="size-6 shrink-0 text-muted-foreground" />
      ) : (
        <SourceMcpIcon
          mcpServerId={row.member.mcpServerId}
          className="size-6 shrink-0 object-contain"
        />
      )}
      <div className="flex min-w-0 flex-col">
        {row.member.builtinKey ? (
          <Link
            to={routes.mcp.builtin.href(row.member.builtinKey)}
            className="truncate font-medium hover:underline"
          >
            {name}
          </Link>
        ) : row.server ? (
          <Link
            to={routes.mcp.x.overview.href(mcpServerRouteParam(row.server))}
            className="truncate font-medium hover:underline"
          >
            {name}
          </Link>
        ) : (
          <Text className="truncate font-medium">{name}</Text>
        )}
        {slug && (
          <Text muted className="truncate font-mono text-xs">
            {slug}
          </Text>
        )}
      </div>
    </div>
  );
}

/** Member management (list, reorder, add, remove) rendered on the Overview tab. */
export function GatewayMembersSection({
  metaMcpServer,
}: {
  metaMcpServer: MetaMcpServer;
}): JSX.Element {
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const {
    rows,
    isLoading,
    servers,
    isError,
    refetch,
    membersUpdatedAt,
    serversUpdatedAt,
  } = useGatewayMemberRows(metaMcpServer.id);
  const toolsets = useToolsets();

  const [addOpen, setAddOpen] = useState(false);
  const batchState = useMemo<AddBatchState>(
    () => ({ wrappers: new Map(), orders: new Map(), completed: new Set() }),
    // oxlint-disable-next-line react-hooks/exhaustive-deps -- reset retry caches when navigating to another gateway
    [metaMcpServer.id],
  );
  const batchRunning = useRef(false);
  const reconciledAt = useRef(membersUpdatedAt);
  const latestMembersUpdatedAt = useRef(membersUpdatedAt);
  const [removeTarget, setRemoveTarget] = useState<MemberRow | null>(null);
  const [mutating, setMutating] = useState(false);
  const markWrapperWritesSettled = useReconcileWrappers(
    batchState,
    servers,
    serversUpdatedAt,
    mutating,
  );

  useEffect(() => {
    if (membersUpdatedAt) latestMembersUpdatedAt.current = membersUpdatedAt;
    if (
      mutating ||
      !membersUpdatedAt ||
      membersUpdatedAt === reconciledAt.current
    )
      return;
    reconciledAt.current = membersUpdatedAt;
    reconcileCompletedMembers(
      batchState,
      new Set(rows.map((row) => row.member.mcpServerId)),
    );
  }, [batchState, membersUpdatedAt, mutating, rows]);

  const invalidateMembers = () =>
    Promise.all([
      invalidateAllMetaMcpMembers(queryClient, { refetchType: "all" }),
      // The Inspect tab reads list_servers/describe_server straight from the
      // endpoint and caches it; membership changes what those return.
      queryClient.invalidateQueries({ queryKey: ["gatewayInspection"] }),
      queryClient.invalidateQueries({ queryKey: ["gatewayDescribeServer"] }),
      // Adding a toolset mints an mcp_servers wrapper the picker keys on.
      invalidateAllMcpServers(queryClient, { refetchType: "all" }),
    ]);

  const runMutation = async (work: () => Promise<void>, failure: string) => {
    setMutating(true);
    try {
      await work();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : failure);
    } finally {
      // Always refetch: a reorder writes rows one at a time, so a failure
      // partway through still changed the server and the table must not keep
      // rendering the pre-move order.
      await invalidateMembers();
      setMutating(false);
    }
  };

  const handleMove = (index: number, direction: -1 | 1) =>
    runMutation(async () => {
      const plan = planReorder(
        rows.map((row) => row.member),
        index,
        index + direction,
      );
      // Sequential: each call is a full sortOrder write and the plan is
      // consistent only as a whole.
      for (const change of plan) {
        await client.metaMcp.updateMember({
          updateMetaMcpMemberForm: change,
        });
      }
    }, "Failed to reorder servers");

  const handleRemove = (row: MemberRow) =>
    runMutation(async () => {
      await client.metaMcp.removeMember({ id: row.member.id });
      const key = row.server
        ? candidateKey(
            { kind: "server", server: row.server },
            batchState.wrappers,
          )
        : `server-${row.member.mcpServerId}`;
      batchState.completed.delete(key);
      batchState.orders.delete(key);
      setRemoveTarget(null);
      toast.success("Server removed");
    }, "Failed to remove server");

  const handleAdd = async (
    candidates: AddCandidate[],
  ): Promise<AddResult[]> => {
    if (batchRunning.current) return [];
    batchRunning.current = true;
    setMutating(true);
    try {
      return await addCandidateBatch(
        candidates,
        nextSortOrder(rows.map((row) => row.member)),
        batchState,
        {
          createWrapper: async (toolset) => {
            const findWrapper = async () => {
              // Read from the server, not the picker cache: a previous create
              // may have committed even when its response never reached us.
              const mcpServers = memberWrappers(
                (await client.mcpServers.list({ toolsetId: toolset.id }))
                  .mcpServers,
                toolset.id,
              );
              if (mcpServers.length > 1) {
                throw new Error(
                  "Multiple servers use this source. Select the intended existing server.",
                );
              }
              return mcpServers[0]?.id;
            };
            const existing = await findWrapper();
            if (existing) return existing;
            try {
              const wrapper = await client.mcpServers.create({
                createMcpServerForm: {
                  name: toolset.name,
                  toolsetId: toolset.id,
                  visibility: "private",
                },
              });
              return wrapper.id;
            } catch (error) {
              const recovered = await findWrapper();
              if (recovered) return recovered;
              throw error;
            }
          },
          attach: async (mcpServerId, sortOrder) => {
            try {
              await client.metaMcp.addMember({
                addMetaMcpMemberForm: {
                  metaMcpServerId: metaMcpServer.id,
                  mcpServerId,
                  sortOrder,
                },
              });
            } catch (error) {
              // A lost response (or a retry conflict) is success only when a
              // fresh read confirms this exact server's membership.
              const { members } = await client.metaMcp.listMembers({
                metaMcpServerId: metaMcpServer.id,
              });
              if (
                !members.some((member) => member.mcpServerId === mcpServerId)
              ) {
                throw error;
              }
            }
          },
        },
      );
    } finally {
      // Ignore responses received during the writes. Only a subsequent
      // successful refresh can retire optimistic duplicate protection.
      reconciledAt.current = latestMembersUpdatedAt.current;
      markWrapperWritesSettled();
      try {
        await invalidateMembers();
      } catch {
        toast.error(
          "Servers changed, but the list couldn't refresh. Reload to see the latest servers.",
        );
      } finally {
        batchRunning.current = false;
        setMutating(false);
      }
    }
  };

  const indexByMemberId = new Map(
    rows.map((row, index) => [row.member.id, index]),
  );

  // A single member has nothing to reorder against, so the column would be
  // two permanently disabled arrows taking up a fifth of the row.
  const reorderColumn: Column<MemberRow>[] =
    rows.length < 2
      ? []
      : [
          {
            key: "order",
            header: "",
            width: "88px",
            render: (row) => {
              const index = indexByMemberId.get(row.member.id) ?? 0;
              return (
                <RequireScope
                  scope="mcp:write"
                  resourceId={metaMcpServer.id}
                  level="component"
                >
                  <div className="flex items-center gap-1">
                    <Button
                      variant="tertiary"
                      size="sm"
                      disabled={mutating || index === 0}
                      onClick={() => void handleMove(index, -1)}
                      aria-label="Move up"
                    >
                      <Button.Icon>
                        <ArrowUp className="size-4" />
                      </Button.Icon>
                    </Button>
                    <Button
                      variant="tertiary"
                      size="sm"
                      disabled={mutating || index === rows.length - 1}
                      onClick={() => void handleMove(index, 1)}
                      aria-label="Move down"
                    >
                      <Button.Icon>
                        <ArrowDown className="size-4" />
                      </Button.Icon>
                    </Button>
                  </div>
                </RequireScope>
              );
            },
          },
        ];

  const columns: Column<MemberRow>[] = [
    ...reorderColumn,
    {
      key: "server",
      header: "Server",
      render: (row) => <MemberNameCell row={row} />,
    },
    {
      key: "backend",
      header: "Backend",
      render: (row) => <BackendKindTag row={row} />,
      width: "140px",
    },
    {
      key: "status",
      header: "Status",
      render: (row) => (
        <MemberStatusBadge classification={row.classification} />
      ),
      width: "140px",
    },
    {
      key: "actions",
      header: "",
      width: "64px",
      render: (row) => (
        <RequireScope
          scope="mcp:write"
          resourceId={metaMcpServer.id}
          level="component"
        >
          <Button
            variant="destructive-secondary"
            size="sm"
            disabled={mutating}
            onClick={() => setRemoveTarget(row)}
            aria-label="Remove server"
          >
            <Button.Icon>
              <Trash2 className="size-4" />
            </Button.Icon>
          </Button>
        </RequireScope>
      ),
    },
  ];

  return (
    <>
      <Page.Section>
        {/* Section heading under the Overview page title: no eyebrow, smaller
            serif. */}
        <Page.Section.Title area="" className="text-display-xs">
          Servers
        </Page.Section.Title>
        <Page.Section.Description>
          The MCP servers this gateway fronts, in the order agents see them in
          list_servers. Status reflects what the backend can attest; live
          upstream health lands with the proxied runtime.
        </Page.Section.Description>
        <Page.Section.CTA>
          <RequireScope
            scope="mcp:write"
            resourceId={metaMcpServer.id}
            level="component"
          >
            <Button size="sm" onClick={() => setAddOpen(true)}>
              <Button.LeftIcon>
                <Plus />
              </Button.LeftIcon>
              <Button.Text>Add servers</Button.Text>
            </Button>
          </RequireScope>
        </Page.Section.CTA>
        <Page.Section.Body>
          {isLoading ? (
            <SkeletonTable />
          ) : isError ? (
            <div role="alert" className="space-y-2">
              <Text>Couldn't load gateway servers.</Text>
              <Button variant="secondary" onClick={() => void refetch()}>
                <Button.Text>Retry loading</Button.Text>
              </Button>
            </div>
          ) : (
            <Table columns={columns}>
              <Table.Header columns={columns} />
              {rows.length === 0 ? (
                <Table.NoResultsMessage>
                  {/* No padding of its own: Table.NoResultsMessage already
                      insets the cell, and a second layer here made this empty
                      state sit lower than every other one. */}
                  <div className="flex flex-col items-center gap-3">
                    <Text muted>
                      No servers yet. A gateway with no servers exposes its four
                      tools but has nothing to route to.
                    </Text>
                    <RequireScope
                      scope="mcp:write"
                      resourceId={metaMcpServer.id}
                      level="component"
                    >
                      <Button
                        variant="secondary"
                        size="sm"
                        onClick={() => setAddOpen(true)}
                      >
                        <Button.LeftIcon>
                          <Plus className="size-4" />
                        </Button.LeftIcon>
                        <Button.Text>Add servers</Button.Text>
                      </Button>
                    </RequireScope>
                  </div>
                </Table.NoResultsMessage>
              ) : (
                <Table.Body
                  columns={columns}
                  data={rows}
                  rowKey={(row) => row.member.id}
                />
              )}
            </Table>
          )}
        </Page.Section.Body>
      </Page.Section>

      {!isLoading && !isError && (
        <BuiltinGatewayMembersSection
          gatewayId={metaMcpServer.id}
          members={rows.map((row) => row.member)}
        />
      )}

      <AddServersSheet
        key={metaMcpServer.id}
        open={addOpen}
        onOpenChange={(open) => {
          if (!batchRunning.current) setAddOpen(open);
        }}
        servers={servers}
        toolsets={toolsets.isError ? [] : toolsets}
        toolsetsLoading={toolsets.isLoading}
        toolsetsFailed={toolsets.isError}
        isLoading={isLoading}
        loadFailed={isError}
        onRetryLoad={() => {
          void refetch();
          void toolsets.refetch();
        }}
        memberServerIds={new Set(rows.map((row) => row.member.mcpServerId))}
        wrappers={batchState.wrappers}
        onAdd={handleAdd}
        gatewayId={metaMcpServer.id}
        adding={mutating}
        projectId={metaMcpServer.projectId}
      />

      <Dialog
        open={removeTarget !== null}
        onOpenChange={(open) => {
          if (!open) setRemoveTarget(null);
        }}
      >
        <Dialog.Content className="max-w-md">
          <Dialog.Header>
            <Dialog.Title>Remove this server?</Dialog.Title>
            <Dialog.Description>
              {`Agents connected to this gateway lose access to ${
                removeTarget?.server?.name ||
                removeTarget?.member.mcpServerName ||
                "this server"
              }'s tools. The server itself is not deleted. In-flight sessions see it disappear from list_servers.`}
            </Dialog.Description>
          </Dialog.Header>
          <Dialog.Footer>
            <Button
              variant="secondary"
              disabled={mutating}
              onClick={() => setRemoveTarget(null)}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
            <Button
              variant="destructive-primary"
              disabled={mutating}
              onClick={() => {
                if (removeTarget) void handleRemove(removeTarget);
              }}
            >
              {mutating && (
                <Button.LeftIcon>
                  <Loader2 aria-hidden="true" className="size-4 animate-spin" />
                </Button.LeftIcon>
              )}
              <Button.Text>Remove server</Button.Text>
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}

export function AddServersSheet({
  open,
  onOpenChange,
  servers,
  toolsets,
  toolsetsLoading = false,
  toolsetsFailed = false,
  isLoading,
  loadFailed,
  onRetryLoad,
  memberServerIds,
  wrappers,
  onAdd,
  adding,
  projectId,
  gatewayId,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  servers: McpServer[];
  toolsets: ToolsetEntry[];
  toolsetsLoading?: boolean;
  toolsetsFailed?: boolean;
  isLoading: boolean;
  loadFailed: boolean;
  onRetryLoad: () => void;
  memberServerIds: Set<string>;
  wrappers?: ReadonlyMap<string, string>;
  onAdd: (candidates: AddCandidate[]) => Promise<AddResult[]>;
  adding: boolean;
  projectId: string;
  gatewayId: string;
}): JSX.Element {
  const routes = useRoutes();
  const navigate = useNavigate();
  const { hasScope, isLoading: permissionsLoading } = useRBAC();
  const canWrite = !permissionsLoading && hasScope("mcp:write", gatewayId);
  const canCreate = !permissionsLoading && hasScope("mcp:write", projectId);
  const canWriteProject =
    !permissionsLoading && hasScope("project:write", projectId);
  // Match the Catalog page's browse permission; installation checks stay there.
  const canBrowseCatalog =
    !permissionsLoading && (hasScope("project:read") || hasScope("mcp:write"));
  const candidates = useMemo(
    () => buildAddCandidates(servers, toolsets, memberServerIds, ""),
    [servers, toolsets, memberServerIds],
  );
  const options: ServerPickerOption[] = candidates.map((candidate) => {
    const key = candidateKey(candidate, wrappers);
    if (candidate.kind === "toolset") {
      return {
        value: key,
        label: candidateName(candidate),
        icon: (
          <SourceMcpIcon
            mcpServerId={undefined}
            className="size-6 shrink-0 object-contain"
          />
        ),
        slug: candidate.toolset.slug,
        kindLabel: CLASSIFICATION_LABEL.hosted,
        badge: candidate.toolset.mcpEnabled ? undefined : "MCP off",
        description: `${candidate.toolset.slug} · Hosted${candidate.toolset.mcpEnabled ? "" : " · MCP off: excluded until enabled"}${canCreate ? "" : " · Requires project-level MCP write permission"}`,
        disabled: !canWrite || !canCreate,
      };
    }
    const server = candidate.server;
    const classification = classifyMemberServer(server);
    const restriction = server.unproxiedMcpServerId
      ? "Direct-connect servers cannot be added to a gateway."
      : !server.slug
        ? "A server slug is required before adding."
        : classification === "disabled"
          ? "Disabled: excluded until enabled."
          : STATUS_BY_CLASSIFICATION[classification].why;
    return {
      value: key,
      label: candidateName(candidate) || "MCP server",
      icon: (
        <SourceMcpIcon
          mcpServerId={server.id}
          className="size-6 shrink-0 object-contain"
        />
      ),
      slug: server.slug,
      kindLabel: CLASSIFICATION_LABEL[classification],
      badge:
        classification === "hosted" || classification === "proxied"
          ? undefined
          : "Excluded",
      description: `${server.slug || "No slug"} · ${restriction}`,
      disabled:
        !canWrite || !server.slug || Boolean(server.unproxiedMcpServerId),
    };
  });
  const submit = (values: string[]) =>
    onAdd(
      candidates.filter((candidate) =>
        values.includes(candidateKey(candidate, wrappers)),
      ),
    );
  const creationOptions = [
    {
      label: "From the catalog",
      description:
        "Pick a reviewed third-party server — Salesforce, Datadog, Linear, Slack, Okta and more.",
      Icon: Blocks,
      group: "Recommended",
      href: routes.mcp.catalog.href() + "?attachToGateway=" + gatewayId,
      allowed: canBrowseCatalog,
    },
    {
      label: "Hosted remotely",
      description:
        "Add a server that already runs elsewhere by its URL, proxied through Speakeasy.",
      Icon: Cloud,
      group: "Recommended",
      href:
        routes.mcp.add.remote.href() +
        "?attachToGateway=" +
        encodeURIComponent(gatewayId),
      allowed: canCreate,
    },
    {
      label: "Reachable through a tunnel",
      description:
        "Connect a server running inside your own network through a tunnel.",
      Icon: Cable,
      group: "Recommended",
      href:
        routes.mcp.add.tunneled.href() +
        "?attachToGateway=" +
        encodeURIComponent(gatewayId),
      allowed: canCreate,
    },
    {
      label: "From your API",
      description: "Upload an OpenAPI document to generate tools.",
      Icon: FileCode,
      group: "Advanced",
      href:
        routes.mcp.add.openapi.href() +
        "?attachToGateway=" +
        encodeURIComponent(gatewayId),
      allowed: canWriteProject && canCreate,
    },
    {
      label: "From an existing source",
      description:
        "Build a server from an OpenAPI document or function this project already has.",
      Icon: Boxes,
      group: "Advanced",
      href:
        routes.mcp.add.fromSource.href() +
        "?attachToGateway=" +
        encodeURIComponent(gatewayId),
      allowed: canCreate,
    },
    {
      label: "Write custom code",
      description: "Create tools with TypeScript functions.",
      Icon: Code,
      group: "Advanced",
      href:
        routes.mcp.add.function.href() +
        "?attachToGateway=" +
        encodeURIComponent(gatewayId),
      allowed: canWriteProject && canCreate,
    },
  ];
  const creationAllowed = creationOptions.some((option) => option.allowed);
  return (
    <ServerPickerSheet
      open={open}
      onOpenChange={onOpenChange}
      title="Add servers to gateway"
      description="Choose existing servers to add, or get started with a new one."
      options={options}
      isLoading={isLoading}
      loadFailed={loadFailed}
      onRetryLoad={onRetryLoad}
      partiallyLoading={toolsetsLoading}
      hideEmptyState={
        (toolsetsLoading || toolsetsFailed) &&
        (servers.length > 0 || toolsets.length > 0)
      }
      emptyMessage={
        servers.length === 0 && toolsets.length === 0
          ? "No existing servers yet. Create a new server to get started."
          : "All existing servers have already been added."
      }
      notices={
        <>
          {toolsetsLoading && <Text muted>Loading hosted servers…</Text>}
          {toolsetsFailed && (
            <div role="alert" className="space-y-2">
              <Text muted>
                Couldn't load hosted servers. Existing servers are still
                available.
              </Text>
              <Button
                variant="secondary"
                onClick={onRetryLoad}
                disabled={adding}
              >
                <Button.Text>Retry loading hosted servers</Button.Text>
              </Button>
            </div>
          )}
        </>
      }
      canWrite={canWrite}
      permissionMessage="You need MCP write permission on this gateway to add servers."
      adding={adding}
      onSubmit={submit}
      renderCreateSection={({ busy, onMenuPointerDownOutside }) => (
        <section aria-labelledby="add-new-server" className="space-y-3">
          <Text as="h3" id="add-new-server" variant="subheading">
            Create a new server
          </Text>
          <DropdownMenu>
            <DropdownMenuTrigger
              asChild
              disabled={busy || !canWrite || !creationAllowed}
            >
              <Button
                variant="primary"
                disabled={busy || !canWrite || !creationAllowed}
              >
                <Button.LeftIcon>
                  <Plus className="size-4" />
                </Button.LeftIcon>
                <Button.Text>Add new</Button.Text>
                <Button.RightIcon>
                  <ChevronDown className="size-4" />
                </Button.RightIcon>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent
              onPointerDownOutside={(event) =>
                onMenuPointerDownOutside(event.detail.originalEvent)
              }
              align="start"
              className="w-80 max-w-[calc(100vw-2rem)]"
            >
              {["Recommended", "Advanced"].map((group, index) => (
                <Fragment key={group}>
                  {index > 0 && <DropdownMenuSeparator />}
                  <DropdownMenuGroup aria-label={group}>
                    <DropdownMenuLabel className="text-muted-foreground text-xs">
                      {group}
                    </DropdownMenuLabel>
                    {creationOptions
                      .filter((option) => option.group === group)
                      .map((option) => (
                        <DropdownMenuItem
                          key={option.href}
                          className="items-start"
                          textValue={option.label}
                          disabled={busy || !canWrite || !option.allowed}
                          onSelect={() => {
                            if (!busy && canWrite && option.allowed)
                              void navigate(option.href);
                          }}
                        >
                          <option.Icon className="mt-0.5" />
                          <span className="min-w-0">
                            <span className="block">{option.label}</span>
                            <span className="text-muted-foreground mt-0.5 block text-xs leading-relaxed">
                              {option.description}
                            </span>
                          </span>
                        </DropdownMenuItem>
                      ))}
                  </DropdownMenuGroup>
                </Fragment>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        </section>
      )}
    />
  );
}
