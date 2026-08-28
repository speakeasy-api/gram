import { Page } from "@/components/page-layout";
import { SourceMcpIcon } from "@/components/sources/SourceCard";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { SearchBar } from "@/components/ui/SearchBar";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { Table, type Column } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";
import { useSdkClient } from "@/contexts/Sdk";
import { mcpServerRouteParam } from "@/lib/sources";
import { useRoutes } from "@/routes";
import { useNavigate } from "react-router";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { MetaMcpServer } from "@gram/client/models/components/metamcpserver.js";
import { invalidateAllMetaMcpMembers } from "@gram/client/react-query/metaMcpMembers.js";

import { useQueryClient } from "@tanstack/react-query";
import { ArrowDown, ArrowUp, Loader2, Plus, Trash2 } from "lucide-react";
import { useMemo, useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import {
  classifyMemberServer,
  nextSortOrder,
  planReorder,
  type MemberClassification,
  type MemberRow,
} from "./memberRows";
import { useGatewayMemberRows } from "./useGatewayMemberRows";

const CLASSIFICATION_LABEL: Record<MemberClassification, string> = {
  hosted: "Hosted",
  proxied: "Proxied",
  disabled: "Disabled",
  unproxied: "Unproxied",
  slugless: "No slug",
  unknown: "Unknown",
};

// Status is backend-attested only, and each state says why: a badge reading
// "Unknown" with no explanation reads as broken rather than as work the
// runtime hasn't reached yet.
const STATUS_BY_CLASSIFICATION: Record<
  MemberClassification,
  { label: string; variant: "success" | "neutral" | "warning"; why: string }
> = {
  hosted: {
    label: "Available",
    variant: "success",
    why: "Toolset-backed, so the gateway executes its tools in-process.",
  },
  proxied: {
    label: "Unknown",
    variant: "neutral",
    why: "The gateway can't reach this member's upstream yet, so it reports no health rather than guessing. Drill-down and execution arrive with per-upstream credential routing.",
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

function MemberStatusBadge({
  classification,
}: {
  classification: MemberClassification;
}): JSX.Element {
  const status = STATUS_BY_CLASSIFICATION[classification];
  return (
    <SimpleTooltip tooltip={status.why}>
      <Badge variant={status.variant}>
        <Badge.Text>{status.label}</Badge.Text>
      </Badge>
    </SimpleTooltip>
  );
}

function MemberNameCell({ row }: { row: MemberRow }): JSX.Element {
  const routes = useRoutes();
  const name = row.server?.name || row.member.mcpServerName || "MCP server";
  const slug = row.server?.slug ?? row.member.mcpServerSlug;
  return (
    <div className="flex min-w-0 items-center gap-2.5">
      <SourceMcpIcon
        mcpServerId={row.member.mcpServerId}
        className="size-5 shrink-0 object-contain"
      />
      <div className="flex min-w-0 flex-col">
        {row.server ? (
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

/** Read-only members table shared by the Overview tab. */
export function MembersStatusTable({
  rows,
  isLoading,
}: {
  rows: MemberRow[];
  isLoading: boolean;
}): JSX.Element {
  const columns: Column<MemberRow>[] = [
    {
      key: "server",
      header: "Server",
      render: (row) => <MemberNameCell row={row} />,
    },
    {
      key: "backend",
      header: "Backend",
      render: (row) => (
        <Text muted small>
          {CLASSIFICATION_LABEL[row.classification]}
        </Text>
      ),
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
  ];

  if (isLoading) {
    return <SkeletonTable />;
  }

  return (
    <Table columns={columns}>
      <Table.Header columns={columns} />
      {rows.length === 0 ? (
        <Table.NoResultsMessage>
          <div className="px-4 py-6 text-center">
            No members yet. Add MCP servers from the Members tab.
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
  );
}

export function GatewayMembersTab({
  metaMcpServer,
}: {
  metaMcpServer: MetaMcpServer;
}): JSX.Element {
  const routes = useRoutes();
  const navigate = useNavigate();
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const { rows, isLoading, servers } = useGatewayMemberRows(metaMcpServer.id);

  const [addOpen, setAddOpen] = useState(false);
  const [removeTarget, setRemoveTarget] = useState<MemberRow | null>(null);
  const [mutating, setMutating] = useState(false);

  const invalidateMembers = () =>
    Promise.all([
      invalidateAllMetaMcpMembers(queryClient, { refetchType: "all" }),
      // The Inspect tab reads list_servers/describe_server straight from the
      // endpoint and caches it; membership changes what those return.
      queryClient.invalidateQueries({ queryKey: ["gatewayInspection"] }),
      queryClient.invalidateQueries({ queryKey: ["gatewayDescribeServer"] }),
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
    }, "Failed to reorder members");

  const handleRemove = (row: MemberRow) =>
    runMutation(async () => {
      await client.metaMcp.removeMember({ id: row.member.id });
      setRemoveTarget(null);
      toast.success("Member removed");
    }, "Failed to remove member");

  const handleAdd = (server: McpServer) =>
    runMutation(async () => {
      await client.metaMcp.addMember({
        addMetaMcpMemberForm: {
          metaMcpServerId: metaMcpServer.id,
          mcpServerId: server.id,
          sortOrder: nextSortOrder(rows.map((row) => row.member)),
        },
      });
      toast.success(`Added ${server.name || "server"} to the gateway`);
    }, "Failed to add member");

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
                <RequireScope scope="mcp:write" level="component">
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
      render: (row) => (
        <Text muted small>
          {CLASSIFICATION_LABEL[row.classification]}
        </Text>
      ),
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
        <RequireScope scope="mcp:write" level="component">
          <Button
            variant="destructive-secondary"
            size="sm"
            disabled={mutating}
            onClick={() => setRemoveTarget(row)}
            aria-label="Remove member"
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
        <Page.Section.Title>Members</Page.Section.Title>
        <Page.Section.Description>
          The MCP servers this gateway fronts, in the order agents see them in
          list_servers.
        </Page.Section.Description>
        <Page.Section.CTA>
          <RequireScope scope="mcp:write" level="component">
            <Button size="sm" onClick={() => setAddOpen(true)}>
              <Button.LeftIcon>
                <Plus />
              </Button.LeftIcon>
              <Button.Text>Add member</Button.Text>
            </Button>
          </RequireScope>
        </Page.Section.CTA>
        <Page.Section.Body>
          {isLoading ? (
            <SkeletonTable />
          ) : (
            <Table columns={columns}>
              <Table.Header columns={columns} />
              {rows.length === 0 ? (
                <Table.NoResultsMessage>
                  <div className="flex flex-col items-center gap-3 px-4 py-8">
                    <Text muted>
                      No members yet. A gateway with no members exposes its four
                      tools but has nothing to route to.
                    </Text>
                    <RequireScope scope="mcp:write" level="component">
                      <Button
                        variant="secondary"
                        size="sm"
                        onClick={() => setAddOpen(true)}
                      >
                        <Button.LeftIcon>
                          <Plus className="size-4" />
                        </Button.LeftIcon>
                        <Button.Text>Add the first member</Button.Text>
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

      <AddMemberSheet
        open={addOpen}
        onOpenChange={setAddOpen}
        servers={servers}
        memberServerIds={new Set(rows.map((row) => row.member.mcpServerId))}
        onAdd={(server) => void handleAdd(server)}
        onAddFromCatalog={() =>
          void navigate(
            routes.sources.addFromCatalog.href() +
              "?attachToGateway=" +
              metaMcpServer.id,
          )
        }
        adding={mutating}
      />

      <Dialog
        open={removeTarget !== null}
        onOpenChange={(open) => {
          if (!open) setRemoveTarget(null);
        }}
      >
        <Dialog.Content className="max-w-md">
          <Dialog.Header>
            <Dialog.Title>Remove this member?</Dialog.Title>
            <Dialog.Description>
              {`Agents connected to this gateway lose access to ${
                removeTarget?.server?.name ||
                removeTarget?.member.mcpServerName ||
                "this server"
              }'s tools. In-flight sessions see it disappear from list_servers.`}
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
              <Button.Text>Remove member</Button.Text>
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}

function AddMemberSheet({
  open,
  onOpenChange,
  servers,
  memberServerIds,
  onAdd,
  onAddFromCatalog,
  adding,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  servers: McpServer[];
  memberServerIds: Set<string>;
  onAdd: (server: McpServer) => void;
  onAddFromCatalog: () => void;
  adding: boolean;
}): JSX.Element {
  const [search, setSearch] = useState("");

  const candidates = useMemo(() => {
    const query = search.toLowerCase();
    return servers
      .filter((server) => !memberServerIds.has(server.id))
      .filter(
        (server) =>
          !query ||
          (server.name?.toLowerCase().includes(query) ?? false) ||
          (server.slug?.toLowerCase().includes(query) ?? false),
      )
      .sort((a, b) => (a.name ?? "").localeCompare(b.name ?? ""));
  }, [servers, memberServerIds, search]);

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="flex w-[520px] flex-col sm:max-w-[520px]"
      >
        <SheetHeader className="px-6 pt-6 pb-0">
          <SheetTitle>Add member</SheetTitle>
          <SheetDescription>
            Front another MCP server through this gateway. Disabled, unproxied
            and slugless servers can be added but are excluded from serving.
          </SheetDescription>
        </SheetHeader>

        <div className="flex items-center gap-2 px-6 pt-4">
          <div className="flex-1">
            <SearchBar
              value={search}
              onChange={setSearch}
              placeholder="Search MCP servers..."
            />
          </div>
          <Button variant="secondary" onClick={onAddFromCatalog}>
            <Button.Text>Add from catalog</Button.Text>
          </Button>
        </div>

        <div className="flex-1 space-y-2 overflow-y-auto px-6 py-4">
          {candidates.length === 0 ? (
            <Text muted className="py-8 text-center">
              {search
                ? `No MCP servers matching \u201c${search}\u201d`
                : "Every MCP server is already a member."}
            </Text>
          ) : (
            candidates.map((server) => {
              const classification = classifyMemberServer(server);
              const servable =
                classification === "hosted" || classification === "proxied";
              return (
                <div
                  key={server.id}
                  className="border-border/60 hover:border-border hover:bg-muted/40 trans flex items-center gap-3 border px-3 py-2.5"
                >
                  <SourceMcpIcon
                    mcpServerId={server.id}
                    className="size-6 shrink-0 object-contain"
                  />
                  <div className="flex min-w-0 flex-1 flex-col">
                    <Text className="truncate text-sm font-medium">
                      {server.name || "MCP server"}
                    </Text>
                    <div className="flex items-center gap-2">
                      {server.slug && (
                        <Text muted className="truncate font-mono text-xs">
                          {server.slug}
                        </Text>
                      )}
                      <Text muted className="text-xs">
                        {CLASSIFICATION_LABEL[classification]}
                      </Text>
                    </div>
                  </div>
                  {!servable && (
                    <Badge variant="warning">
                      <Badge.Text>Excluded</Badge.Text>
                    </Badge>
                  )}
                  <Button
                    variant="secondary"
                    size="sm"
                    disabled={adding}
                    onClick={() => onAdd(server)}
                  >
                    <Button.Text>Add</Button.Text>
                  </Button>
                </div>
              );
            })
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}
