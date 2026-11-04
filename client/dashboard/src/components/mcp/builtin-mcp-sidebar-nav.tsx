import {
  DetailSidebarInfoLabel,
  DetailSidebarNav,
  type DetailSidebarNavItem,
} from "@/components/detail/detail-sidebar-nav";
import { useOrganization } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useBuiltinMcpCredentials } from "@/components/mcp/useBuiltinMcpCredentials";
import { useBuiltinMcpServers } from "@/components/mcp/useBuiltinMcpServers";
import { useUpdateBuiltinMcpServer } from "@/components/mcp/useUpdateBuiltinMcpServer";
import { SidebarUrlRow } from "@/components/mcp-server-x-sidebar-nav";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { Text } from "@/components/ui/Text";
import { cn } from "@/lib/utils";
import { useRoutes } from "@/routes";
import type { BuiltinMcpServer } from "@gram/client/models/components/builtinmcpserver.js";
import {
  Check,
  ChevronDown,
  KeyRound,
  LayoutDashboard,
  Wrench,
} from "lucide-react";
import * as React from "react";
import { Link, useLocation, useParams } from "react-router";
import {
  activeBuiltinTabFromPath,
  builtinMcpTabHref,
} from "@/pages/mcp/BuiltinMcpServerRouting";

/**
 * The built-in server's rail. Same bones as the other MCP server rails — back
 * link, an at-a-glance card, then the server's sections — so moving between a
 * built-in and your own servers does not change where anything is.
 */
export function BuiltinMcpSidebarNav(): React.JSX.Element | null {
  const routes = useRoutes();
  const location = useLocation();
  const { builtinKey } = useParams<{ builtinKey: string }>();
  const organization = useOrganization();
  const { hasScope, isLoading: permissionsLoading } = useRBAC();
  const { servers } = useBuiltinMcpServers();
  const server = servers.find((entry) => entry.key === builtinKey);
  const credentials = useBuiltinMcpCredentials(server);

  // The page behind this rail is org:admin only; without the same check a
  // non-admin opening the URL gets its navigation and its lookups.
  if (!builtinKey || permissionsLoading) return null;
  if (!hasScope("org:admin", organization.id)) return null;

  const activeTab = activeBuiltinTabFromPath(location.pathname, builtinKey);
  const authenticationHref = builtinMcpTabHref(
    routes,
    builtinKey,
    "authentication",
  );
  const needsCredentials =
    !!server &&
    !credentials.isLoading &&
    !credentials.isError &&
    !credentials.ready;

  const items: DetailSidebarNavItem[] = [
    {
      key: "overview",
      title: "Overview",
      Icon: LayoutDashboard,
      href: builtinMcpTabHref(routes, builtinKey, "overview"),
      active: activeTab === "overview" || activeTab === undefined,
    },
    {
      key: "tools",
      title: "Tools",
      Icon: Wrench,
      href: builtinMcpTabHref(routes, builtinKey, "tools"),
      active: activeTab === "tools",
    },
    {
      key: "authentication",
      title: "Authentication",
      Icon: KeyRound,
      titleNode: (
        <span className="flex items-center gap-1.5">
          Authentication
          {needsCredentials && (
            <span
              aria-hidden="true"
              className="bg-warning-foreground size-1.5 shrink-0 rounded-full"
            />
          )}
        </span>
      ),
      href: authenticationHref,
      active: activeTab === "authentication",
    },
  ];

  const cardContent = server && (
    <>
      <div className="flex min-w-0 flex-col gap-1">
        <Text className="truncate font-semibold">{server.name}</Text>
        <DetailSidebarInfoLabel>Built-in</DetailSidebarInfoLabel>
      </div>

      <div className="flex flex-col gap-1.5">
        <DetailSidebarInfoLabel>Visibility</DetailSidebarInfoLabel>
        <BuiltinVisibilityDropdown server={server} />
      </div>

      <div className="flex flex-col gap-1.5">
        <DetailSidebarInfoLabel>Credentials</DetailSidebarInfoLabel>
        {credentials.isLoading ? (
          <Text variant="small" muted>
            Checking…
          </Text>
        ) : credentials.isError ? (
          <Text variant="small" muted>
            Unavailable
          </Text>
        ) : credentials.ready ? (
          <Text variant="small">Ready</Text>
        ) : (
          // Text sets its own color, so the warning tone goes on a plain
          // element rather than losing to it in source order.
          <Link
            to={authenticationHref}
            className="text-default-warning w-fit text-sm"
          >
            Needs setup
          </Link>
        )}
      </div>

      <SidebarUrlRow label="URL" url={server.url} copyTooltip="Copy URL" />

      <div className="flex flex-col gap-1">
        <DetailSidebarInfoLabel>Tools</DetailSidebarInfoLabel>
        <Text variant="small">{server.tools.length}</Text>
      </div>
    </>
  );

  return (
    <DetailSidebarNav
      backHref={routes.mcp.href()}
      cardContent={cardContent}
      items={items}
    />
  );
}

const VISIBILITY_OPTIONS: {
  hidden: boolean;
  label: string;
  description: string;
  dotClass: string;
}[] = [
  {
    hidden: false,
    label: "Shown",
    description:
      "Listed with this project's MCP servers and findable from the command palette.",
    dotClass: "bg-success-default",
  },
  {
    hidden: true,
    label: "Hidden",
    description:
      "Kept out of the lists. The address, permissions, and gateway membership are unchanged.",
    dotClass: "bg-information-default",
  },
];

/**
 * Visibility is a view preference, not an access control, so it applies the
 * moment it is picked — like the status control on your own servers.
 */
function BuiltinVisibilityDropdown({ server }: { server: BuiltinMcpServer }) {
  const update = useUpdateBuiltinMcpServer();
  const current =
    VISIBILITY_OPTIONS.find((option) => option.hidden === server.hidden) ??
    VISIBILITY_OPTIONS[0]!;

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild disabled={update.isPending}>
        <button
          type="button"
          disabled={update.isPending}
          aria-label={`Visibility: ${current.label}`}
          className="text-foreground hover:bg-muted trans border-border flex w-fit items-center gap-2 border px-3 py-1.5 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
        >
          <span
            aria-hidden="true"
            className={cn("size-2 shrink-0 rounded-full", current.dotClass)}
          />
          {current.label}
          <ChevronDown className="text-muted-foreground size-3" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-[320px] p-1">
        {VISIBILITY_OPTIONS.map((option) => (
          <DropdownMenuItem
            key={option.label}
            onSelect={() =>
              update.save({
                key: server.key,
                expectedVersion: server.version,
                hidden: option.hidden,
                environmentId: server.environmentId,
              })
            }
            className="group flex cursor-pointer items-start gap-2.5 p-2"
          >
            {option.hidden === server.hidden ? (
              <span
                className={cn(
                  "mt-1 flex size-3.5 shrink-0 items-center justify-center rounded-full",
                  option.dotClass,
                )}
              >
                <Check
                  className="text-background size-2.5"
                  strokeWidth={4}
                  aria-hidden="true"
                />
              </span>
            ) : (
              <span className="border-border mt-1 size-3.5 shrink-0 rounded-full border" />
            )}
            <div className="flex flex-col gap-0.5">
              <Text small className="font-medium">
                {option.label}
              </Text>
              <Text muted small>
                {option.description}
              </Text>
            </div>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
