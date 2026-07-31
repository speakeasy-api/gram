import { PublicMcpWarningDialog } from "@/components/public-mcp-warning-dialog";
import { ServerEnableDialog } from "@/components/server-enable-dialog";
import { Dialog } from "@/components/ui/Dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRBAC } from "@/hooks/useRBAC";
import { useToolset } from "@/hooks/toolTypes";
import { ONBOARD_EXTERNAL_MCP_TO_USER_SESSIONS_FLAG } from "@/lib/externalMcpUserSessions";
import { getTunneledMcpServerArgs } from "@/lib/sources";
import { cn } from "@/lib/utils";
import { getSystemProvidedVariables } from "@/pages/mcp/environmentVariableUtils";
import {
  getOAuthParadigm,
  isUserSessionIssuerWired,
  mustConvertOAuthBeforePrivate,
} from "@/pages/mcp/toolsetAuthSurface";
import { useEnvironmentVariables } from "@/pages/mcp/useEnvironmentVariables";
import type {
  McpServer,
  McpServerVisibility,
} from "@gram/client/models/components/mcpserver.js";
import { useGetMcpMetadata } from "@gram/client/react-query/getMcpMetadata.js";
import { invalidateAllGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { useGetTunneledMcpServer } from "@gram/client/react-query/getTunneledMcpServer.js";
import { useListEnvironments } from "@gram/client/react-query/listEnvironments.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { invalidateAllPlugins } from "@gram/client/react-query/plugins";
import { invalidateAllPublishStatus } from "@gram/client/react-query/publishStatus";
import { invalidateAllToolset } from "@gram/client/react-query/toolset.js";
import { useUpdateMcpServerMutation } from "@gram/client/react-query/updateMcpServer.js";
import { Button } from "@/components/ui/Button";
import { useQueryClient } from "@tanstack/react-query";
import { Check, ChevronDown } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";

type VisibilityOption = {
  value: McpServerVisibility;
  label: string;
  description: string;
  dotClass: string;
  hoverDotClass: string;
};

const VISIBILITY_OPTIONS: VisibilityOption[] = [
  {
    value: "disabled",
    label: "Disabled",
    description: "This server is offline. No users can connect to it",
    dotClass: "bg-amber-400",
    hoverDotClass: "group-hover:bg-amber-400",
  },
  {
    value: "private",
    label: "Private",
    description: "The server serves traffic.",
    dotClass: "bg-blue-400",
    hoverDotClass: "group-hover:bg-blue-400",
  },
];

// Public visibility for tunneled-backed servers requires the tunnel source
// owner's consent (double opt-in) and serves anonymously.
const TUNNELED_PUBLIC_OPTION: VisibilityOption = {
  value: "public",
  label: "Public",
  description:
    "Anyone can connect anonymously — no login. Every tool is exposed to the public internet.",
  dotClass: "bg-green-400",
  hoverDotClass: "group-hover:bg-green-400",
};

// Toolset-backed public servers expose tool listings but still authenticate
// tool execution.
const TOOLSET_PUBLIC_OPTION: VisibilityOption = {
  value: "public",
  label: "Public",
  description:
    "Anyone with the URL can read the tools hosted by this server. Authentication is still required to use the tools.",
  dotClass: "bg-green-400",
  hoverDotClass: "group-hover:bg-green-400",
};

function visibilityToast(next: McpServerVisibility): string {
  switch (next) {
    case "disabled":
      return "MCP server disabled";
    case "public":
      return "MCP server set to public";
    case "private":
      return "MCP server set to private";
  }
}

export function MCPServerStatusDropdown({
  server,
}: {
  server: McpServer;
}): JSX.Element {
  const { hasScope } = useRBAC();
  const canWrite = hasScope("mcp:write");
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();
  const [dropdownOpen, setDropdownOpen] = useState(false);
  const [pendingEnable, setPendingEnable] =
    useState<McpServerVisibility | null>(null);
  const [publicWarningPending, setPublicWarningPending] = useState<{
    target: McpServerVisibility;
    sourceVisibility: McpServerVisibility;
  } | null>(null);
  const [convertOAuthBlockOpen, setConvertOAuthBlockOpen] = useState(false);

  const isToolsetBacked = server.backendKind === "toolset";
  const toolsetSlug = server.toolsetSummary?.slug;
  const { data: toolset } = useToolset(
    isToolsetBacked ? toolsetSlug : undefined,
  );

  const update = useUpdateMcpServerMutation({
    onSuccess: async (_data, variables) => {
      await Promise.all([
        invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
        // Toolset publishing state reads the wrapper's visibility, so stale
        // toolset views must refresh alongside the server queries.
        invalidateAllToolset(queryClient, { refetchType: "all" }),
        // Enabling a disabled server (e.g. disabled -> private) auto-attaches
        // it to the Default plugin server-side, which the plugin banner's
        // membership check and publish-freshness state need to pick up.
        invalidateAllPlugins(queryClient, { refetchType: "all" }),
        invalidateAllPublishStatus(queryClient, { refetchType: "all" }),
      ]);
      const next = variables.request.updateMcpServerForm.visibility;
      toast.success(visibilityToast(next));
    },
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to update server visibility",
      );
    },
  });

  // Toolset-backed servers gate the Public option on system-provided env
  // vars: going public can leak platform-managed credentials, so warn before
  // the visibility change is committed.
  const { data: environmentsData } = useListEnvironments(undefined, undefined, {
    enabled: isToolsetBacked,
    throwOnError: false,
  });
  const environments = useMemo(
    () => environmentsData?.environments ?? [],
    [environmentsData],
  );
  const { data: mcpMetadataData, isLoading: mcpMetadataLoading } =
    useGetMcpMetadata({ mcpServerId: server.id }, undefined, {
      enabled: isToolsetBacked,
      retry: false,
      throwOnError: false, // Expected 404 when no metadata exists
    });
  const mcpMetadata = mcpMetadataData?.metadata;
  const attachedEnvironment = mcpMetadata?.defaultEnvironmentId
    ? (environments.find((e) => e.id === mcpMetadata.defaultEnvironmentId) ??
      null)
    : null;
  const envVars = useEnvironmentVariables(toolset, environments, mcpMetadata);
  const systemVarNames = useMemo(
    () =>
      attachedEnvironment
        ? getSystemProvidedVariables(envVars, attachedEnvironment.slug)
        : [],
    [envVars, attachedEnvironment],
  );

  // While metadata/environments are still loading we can't tell whether system
  // vars exist, so hold the Public option to prevent bypassing the warning. A
  // metadata 404 (no metadata row) is a safe, common state and not a blocker.
  const publicOptionUnavailable =
    isToolsetBacked && (mcpMetadataLoading || !environmentsData);

  const applyVisibility = (next: McpServerVisibility) => {
    update.mutate({
      request: {
        updateMcpServerForm: {
          id: server.id,
          name: server.name ?? undefined,
          remoteMcpServerId: server.remoteMcpServerId ?? undefined,
          tunneledMcpServerId: server.tunneledMcpServerId ?? undefined,
          toolsetId: server.toolsetId ?? undefined,
          environmentId: server.environmentId ?? undefined,
          // updateMcpServer is a full-record replace for the optional UUID
          // references. Forwarding them keeps stored values intact across a
          // visibility-only update.
          toolVariationsGroupId: server.toolVariationsGroupId ?? undefined,
          visibility: next,
        },
      },
    });
  };

  const handleSelect = (next: McpServerVisibility) => {
    if (next === server.visibility) return;
    if (!isToolsetBacked) {
      applyVisibility(next);
      return;
    }

    setDropdownOpen(false);

    const goingPublic = next === "public";
    const needsEnableDialog =
      next === "disabled" || server.visibility === "disabled";
    const needsPublicWarning = goingPublic && systemVarNames.length > 0;
    // Private OAuth requires a wired session issuer under the user-sessions
    // flag, so a public→private flip is blocked until conversion.
    const needsConvertBlock =
      next === "private" &&
      !!toolset &&
      mustConvertOAuthBeforePrivate({
        flagEnabled:
          telemetry.isFeatureEnabled(
            ONBOARD_EXTERNAL_MCP_TO_USER_SESSIONS_FLAG,
          ) ?? false,
        mcpIsPublic: server.visibility === "public",
        userSessionIssuerWired: isUserSessionIssuerWired(toolset),
        oauthParadigm: getOAuthParadigm(toolset),
      });

    // Defer state changes until after the dropdown has fully closed to avoid
    // Radix focus-trap conflicts.
    setTimeout(() => {
      if (needsConvertBlock) {
        setConvertOAuthBlockOpen(true);
      } else if (needsPublicWarning) {
        // Show the system-vars warning first. If the user confirms, we chain
        // to ServerEnableDialog when the transition also requires enablement.
        setPublicWarningPending({
          target: next,
          sourceVisibility: server.visibility,
        });
      } else if (needsEnableDialog) {
        setPendingEnable(next);
      } else {
        applyVisibility(next);
      }
    }, 0);
  };

  const handlePublicWarningConfirm = () => {
    const pending = publicWarningPending;
    setPublicWarningPending(null);
    if (!pending) return;
    // Use the source visibility captured when the dialog opened, not the live
    // value — the server query may have revalidated in the meantime.
    if (pending.sourceVisibility === "disabled") {
      setPendingEnable(pending.target);
    } else {
      applyVisibility(pending.target);
    }
  };

  const currentLabel =
    server.visibility === "disabled"
      ? "Disabled"
      : server.visibility === "public"
        ? "Public"
        : "Private";

  const isTunneled = Boolean(server.tunneledMcpServerId);
  const { data: tunneledSource } = useGetTunneledMcpServer(
    getTunneledMcpServerArgs(server.tunneledMcpServerId ?? ""),
    undefined,
    { enabled: isTunneled },
  );
  const sourceAllowsPublic = tunneledSource?.allowPublic ?? false;

  let options = VISIBILITY_OPTIONS;
  if (isTunneled) {
    options = [...VISIBILITY_OPTIONS, TUNNELED_PUBLIC_OPTION];
  } else if (isToolsetBacked) {
    options = [...VISIBILITY_OPTIONS, TOOLSET_PUBLIC_OPTION];
  }

  const currentDotClass =
    options.find((option) => option.value === server.visibility)?.dotClass ??
    "bg-green-400";

  return (
    <>
      <DropdownMenu open={dropdownOpen} onOpenChange={setDropdownOpen}>
        <DropdownMenuTrigger asChild disabled={!canWrite || update.isPending}>
          <button
            type="button"
            disabled={!canWrite || update.isPending}
            className="text-foreground hover:bg-muted trans border-border flex w-fit items-center gap-2 rounded-md border px-3 py-1.5 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
          >
            <span
              className={cn("h-2 w-2 shrink-0 rounded-full", currentDotClass)}
            />
            {currentLabel}
            <ChevronDown className="text-muted-foreground h-3 w-3" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" className="w-[320px] p-1">
          {options.map((option) => {
            // Public is gated on the tunnel source's consent or, for
            // toolset-backed servers, on the env data needed for the
            // system-vars warning. Render it disabled with a hint rather than
            // hiding it, so owners know the toggle exists.
            const publicBlocked =
              option.value === "public" &&
              ((isTunneled && !sourceAllowsPublic) || publicOptionUnavailable);
            let description = option.description;
            if (option.value === "public" && publicBlocked) {
              description = publicOptionUnavailable
                ? "Loading environment data…"
                : "Enable public access on the tunnel source first to allow anonymous serving.";
            }
            return (
              <DropdownMenuItem
                key={option.value}
                disabled={publicBlocked}
                onSelect={() => {
                  if (publicBlocked) return;
                  handleSelect(option.value);
                }}
                className="group flex cursor-pointer items-start gap-2.5 rounded-md p-2 data-[disabled]:cursor-not-allowed data-[disabled]:opacity-60"
              >
                {option.value === server.visibility ? (
                  <span
                    className={cn(
                      "mt-1 flex size-3.5 shrink-0 items-center justify-center rounded-full",
                      option.dotClass,
                    )}
                  >
                    <Check
                      className="text-background h-2.5 w-2.5"
                      strokeWidth={4}
                    />
                  </span>
                ) : (
                  <span
                    className={cn(
                      "mt-1 size-3.5 shrink-0 rounded-full transition-colors",
                      "bg-muted",
                      option.hoverDotClass,
                    )}
                  />
                )}
                <div className="flex-1">
                  <span className="block font-mono text-xs font-semibold tracking-wide uppercase">
                    {option.label}
                  </span>
                  <span className="text-muted-foreground text-xs">
                    {description}
                  </span>
                </div>
              </DropdownMenuItem>
            );
          })}
        </DropdownMenuContent>
      </DropdownMenu>
      <Dialog
        open={convertOAuthBlockOpen}
        onOpenChange={setConvertOAuthBlockOpen}
      >
        <Dialog.Content className="max-w-md">
          <Dialog.Header>
            <Dialog.Title>Convert OAuth configuration first</Dialog.Title>
            <Dialog.Description>
              The existing OAuth configuration must be converted to a session
              issuer before this MCP server can be made private. You can convert
              it from the Authentication tab.
            </Dialog.Description>
          </Dialog.Header>
          <Dialog.Footer>
            <Button onClick={() => setConvertOAuthBlockOpen(false)}>
              Close
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
      <PublicMcpWarningDialog
        isOpen={publicWarningPending !== null}
        onClose={() => setPublicWarningPending(null)}
        onConfirm={handlePublicWarningConfirm}
        isLoading={update.isPending}
        environmentSlug={attachedEnvironment?.slug ?? ""}
        variableNames={systemVarNames}
      />
      <ServerEnableDialog
        isOpen={pendingEnable !== null}
        onClose={() => setPendingEnable(null)}
        onConfirm={() => {
          if (pendingEnable) applyVisibility(pendingEnable);
          setPendingEnable(null);
        }}
        isLoading={update.isPending}
        currentlyEnabled={server.visibility !== "disabled"}
        targetIsPublic={pendingEnable === "public"}
      />
    </>
  );
}
