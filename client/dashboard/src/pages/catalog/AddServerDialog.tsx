import { Label } from "@/components/ui/label";
import { Type } from "@/components/ui/type";
import { cn, getServerURL } from "@/lib/utils";
import type { Server } from "@/pages/catalog/hooks";
import { useRoutes } from "@/routes";
import { Button, Dialog, Input, Stack } from "@speakeasy-api/moonshine";
import {
  AlertCircle,
  ArrowRight,
  Check,
  Circle,
  ExternalLink,
  Loader2,
  MessageCircle,
  Plug,
  Plus,
  Server as ServerIcon,
  Settings,
  X,
} from "lucide-react";
import { useEffect, useRef } from "react";
import {
  type ExternalMcpReleaseState,
  type ServerToolsetStatus,
  useExternalMcpReleaseState,
} from "./useExternalMcpReleaseState";

export interface AddServerDialogProps {
  servers: Server[];
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onServersAdded?: () => void;
  projectSlug?: string;
  projectSelector?: React.ReactNode;
}

export function AddServerDialog({
  servers,
  open,
  onOpenChange,
  onServersAdded,
  projectSlug,
  projectSelector,
}: AddServerDialogProps) {
  const releaseState = useExternalMcpReleaseState({
    servers,
    projectSlug,
  });

  // Reset when dialog closes
  useEffect(() => {
    if (!open) {
      releaseState.reset();
    }
  }, [open]);

  // Notify parent when all toolsets are done
  const allToolsetsDone =
    releaseState.toolsetStatuses.length > 0 &&
    releaseState.toolsetStatuses.every(
      (s) => s.status === "completed" || s.status === "failed",
    );
  const prevAllDoneRef = useRef(false);
  useEffect(() => {
    if (allToolsetsDone && !prevAllDoneRef.current) {
      prevAllDoneRef.current = true;
      onServersAdded?.();
    }
    if (!allToolsetsDone) {
      prevAllDoneRef.current = false;
    }
  }, [allToolsetsDone]);

  if (servers.length === 0) return null;

  const isSingle = servers.length === 1;
  const title =
    releaseState.phase === "complete"
      ? "Added to Project"
      : releaseState.phase === "error"
        ? "Deployment Error"
        : isSingle
          ? "Add to Project"
          : `Add ${servers.length} servers to project`;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="gap-2">
        <Dialog.Header>
          <Dialog.Title>{title}</Dialog.Title>
          <Dialog.Description>
            <PhaseDescription phase={releaseState.phase} isSingle={isSingle} />
          </Dialog.Description>
        </Dialog.Header>
        <PhaseContent
          releaseState={releaseState}
          isSingle={isSingle}
          projectSelector={projectSelector}
          onClose={() => onOpenChange(false)}
        />
      </Dialog.Content>
    </Dialog>
  );
}

function PhaseDescription({
  phase,
  isSingle,
}: {
  phase: ExternalMcpReleaseState["phase"];
  isSingle: boolean;
}) {
  switch (phase) {
    case "configure":
      return isSingle
        ? "Add this MCP server to your project."
        : "Configure and add these MCP servers to your project.";
    case "deploying":
      return "Deploying MCP server configuration...";
    case "complete":
      return "";
    case "error":
      return "";
  }
}

function PhaseContent({
  releaseState,
  isSingle,
  projectSelector,
  onClose,
}: {
  releaseState: ExternalMcpReleaseState;
  isSingle: boolean;
  projectSelector?: React.ReactNode;
  onClose: () => void;
}) {
  switch (releaseState.phase) {
    case "configure":
      return (
        <ConfigurePhase
          releaseState={releaseState}
          isSingle={isSingle}
          projectSelector={projectSelector}
          onClose={onClose}
        />
      );
    case "deploying":
      return <DeployingPhase releaseState={releaseState} />;
    case "complete":
      return (
        <CompletePhase
          releaseState={releaseState}
          isSingle={isSingle}
          onClose={onClose}
        />
      );
    case "error":
      return <ErrorPhase releaseState={releaseState} onClose={onClose} />;
  }
}

/** Routes scoped to the target project (which may differ from the current project). */
function useTargetRoutes(releaseState: ExternalMcpReleaseState) {
  return useRoutes(
    releaseState.projectSlug
      ? { projectSlug: releaseState.projectSlug }
      : undefined,
  );
}

// --- Configure Phase ---

function ConfigurePhase({
  releaseState,
  isSingle,
  projectSelector,
  onClose,
}: {
  releaseState: ExternalMcpReleaseState;
  isSingle: boolean;
  projectSelector?: React.ReactNode;
  onClose: () => void;
}) {
  const routes = useTargetRoutes(releaseState);
  const { existingSpecifiers } = releaseState;
  const allAlreadyAdded =
    existingSpecifiers.size > 0 &&
    releaseState.serverConfigs.every((c) =>
      existingSpecifiers.has(c.server.registrySpecifier),
    );
  const hasNewServers = releaseState.serverConfigs.some(
    (c) => !existingSpecifiers.has(c.server.registrySpecifier),
  );

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && releaseState.canDeploy && hasNewServers) {
      e.preventDefault();
      releaseState.startDeployment();
    }
  };

  return (
    <div onKeyDown={handleKeyDown}>
      <Stack gap={4} className="py-2">
        {projectSelector}
        {allAlreadyAdded ? (
          <div className="flex items-start gap-3 p-3 rounded-lg border bg-muted/30">
            <AlertCircle className="size-4 text-muted-foreground shrink-0 mt-0.5" />
            <Type small muted>
              {releaseState.serverConfigs.length === 1
                ? "This source is"
                : "All selected sources are"}{" "}
              already in your project.{" "}
              <routes.sources.Link className="text-primary">
                View sources
              </routes.sources.Link>
            </Type>
          </div>
        ) : isSingle ? (
          <SingleServerConfig releaseState={releaseState} />
        ) : (
          <BatchServerConfig releaseState={releaseState} />
        )}
      </Stack>
      <Dialog.Footer>
        <Button variant="tertiary" onClick={onClose}>
          {allAlreadyAdded ? "Close" : "Cancel"}
        </Button>
        {!allAlreadyAdded && (
          <Button
            disabled={!releaseState.canDeploy || !hasNewServers}
            onClick={() => releaseState.startDeployment()}
          >
            <Button.Text>Deploy</Button.Text>
          </Button>
        )}
      </Dialog.Footer>
    </div>
  );
}

function SingleServerConfig({
  releaseState,
}: {
  releaseState: ExternalMcpReleaseState;
}) {
  const config = releaseState.serverConfigs[0];
  if (!config) return null;

  return (
    <div className="flex flex-col gap-2">
      <Label>Source name</Label>
      <Input
        placeholder={config.server.title || config.server.registrySpecifier}
        value={config.name}
        onChange={(e) =>
          releaseState.updateServerConfig(0, { name: e.target.value })
        }
      />
    </div>
  );
}

function BatchServerConfig({
  releaseState,
}: {
  releaseState: ExternalMcpReleaseState;
}) {
  const { existingSpecifiers } = releaseState;
  return (
    <div className="space-y-3 max-h-80 overflow-y-auto">
      {releaseState.serverConfigs.map((config, index) => {
        const isAlreadyAdded = existingSpecifiers.has(
          config.server.registrySpecifier,
        );
        return (
          <div
            key={config.server.registrySpecifier}
            className={cn(
              "flex items-center gap-3 p-3 rounded-lg border",
              isAlreadyAdded && "opacity-50",
            )}
          >
            <div className="w-6 h-6 rounded bg-primary/10 flex items-center justify-center shrink-0">
              {config.server.iconUrl ? (
                <img
                  src={config.server.iconUrl}
                  alt=""
                  className="w-4 h-4 rounded"
                />
              ) : (
                <ServerIcon className="w-3 h-3 text-muted-foreground" />
              )}
            </div>
            <div className="flex-1 min-w-0">
              <Input
                placeholder={
                  config.server.title || config.server.registrySpecifier
                }
                value={config.name}
                onChange={(e) =>
                  releaseState.updateServerConfig(index, {
                    name: e.target.value,
                  })
                }
                className="text-sm"
              />
            </div>
            {isAlreadyAdded && (
              <span className="text-xs text-muted-foreground shrink-0">
                Already added
              </span>
            )}
          </div>
        );
      })}
    </div>
  );
}

// --- Deploying Phase ---

function DeployingPhase({
  releaseState,
}: {
  releaseState: ExternalMcpReleaseState;
}) {
  const logsEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    logsEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [releaseState.deploymentLogs.length]);

  const statusText = (() => {
    const s = releaseState.deploymentStatus;
    if (!s || s === "created") return "Waiting for deployment to start...";
    if (s === "pending") return "Processing deployment...";
    return "Deploying...";
  })();

  return (
    <div className="py-2 space-y-4">
      <Stack direction="horizontal" gap={2} align="center">
        <Loader2 className="w-4 h-4 animate-spin text-muted-foreground" />
        <Type small muted>
          {statusText}
        </Type>
      </Stack>
      {releaseState.deploymentLogs.length > 0 && (
        <div className="rounded-lg border bg-muted/30 p-3 max-h-48 overflow-y-auto font-mono text-xs space-y-1">
          {releaseState.deploymentLogs.map((log) => (
            <div
              key={log.id}
              className={log.event.includes("error") ? "text-destructive" : ""}
            >
              {log.message}
            </div>
          ))}
          <div ref={logsEndRef} />
        </div>
      )}
    </div>
  );
}

// --- Complete Phase ---

function CompletePhase({
  releaseState,
  onClose,
}: {
  releaseState: ExternalMcpReleaseState;
  isSingle: boolean;
  onClose: () => void;
}) {
  const allDone = releaseState.toolsetStatuses.every(
    (s) => s.status === "completed" || s.status === "failed",
  );

  return (
    <div className="pb-2 space-y-4">
      {/* Toolset creation progress */}
      <div>
        <Type className="font-medium mb-2">Creating MCP servers</Type>
        <div className="space-y-2">
          {releaseState.toolsetStatuses.map((ts) => (
            <ToolsetStatusRow
              key={ts.slug}
              status={ts}
              releaseState={releaseState}
            />
          ))}
        </div>
      </div>

      {/* Next steps — only shown when all toolsets are done */}
      {allDone &&
        (() => {
          const firstCompleted = releaseState.toolsetStatuses.find(
            (s) => s.status === "completed" && s.toolsetSlug && s.mcpSlug,
          );
          if (firstCompleted) {
            return (
              <SingleServerNextSteps
                toolsetSlug={firstCompleted.toolsetSlug!}
                mcpSlug={firstCompleted.mcpSlug!}
                releaseState={releaseState}
              />
            );
          }
          return (
            <Dialog.Footer>
              <Button variant="tertiary" onClick={onClose}>
                <Button.Text>Close</Button.Text>
              </Button>
            </Dialog.Footer>
          );
        })()}
    </div>
  );
}

function ToolsetStatusRow({
  status,
  releaseState,
}: {
  status: ServerToolsetStatus;
  releaseState: ExternalMcpReleaseState;
}) {
  const routes = useTargetRoutes(releaseState);
  const isCompleted = status.status === "completed" && status.toolsetSlug;

  const content = (
    <div className="flex items-center gap-3 p-2 rounded-lg border">
      <div className="flex-1 min-w-0 flex items-center gap-2">
        <Type small className="truncate">
          {status.name}
        </Type>
      </div>
      <div className="flex items-center gap-2">
        {isCompleted && (
          <ArrowRight className="w-3 h-3 text-muted-foreground" />
        )}
        <ToolsetStatusIcon status={status.status} />
      </div>
    </div>
  );

  if (isCompleted) {
    return (
      <routes.mcp.details.Link
        params={[status.toolsetSlug!]}
        className="no-underline hover:no-underline block hover:opacity-80 transition-opacity"
      >
        {content}
      </routes.mcp.details.Link>
    );
  }

  return content;
}

function ToolsetStatusIcon({
  status,
}: {
  status: ServerToolsetStatus["status"];
}) {
  switch (status) {
    case "pending":
      return <Circle className="w-4 h-4 text-muted-foreground shrink-0" />;
    case "creating":
      return (
        <Loader2 className="w-4 h-4 animate-spin text-muted-foreground shrink-0" />
      );
    case "completed":
      return <Check className="w-4 h-4 text-emerald-500 shrink-0" />;
    case "failed":
      return <X className="w-4 h-4 text-destructive shrink-0" />;
  }
}

function SingleServerNextSteps({
  toolsetSlug,
  mcpSlug,
  releaseState,
}: {
  toolsetSlug: string;
  mcpSlug: string;
  releaseState: ExternalMcpReleaseState;
}) {
  const routes = useTargetRoutes(releaseState);

  return (
    <div>
      <Type className="font-medium mb-2">Next steps</Type>
      <div className="grid grid-cols-2 gap-2">
        <routes.sources.Link className="no-underline hover:no-underline">
          <div className="group flex items-center gap-3 p-3 rounded-lg border hover:border-foreground/20 hover:bg-muted/30 transition-all [&_*]:no-underline h-full">
            <div className="w-8 h-8 rounded-md bg-blue-500/10 dark:bg-blue-500/20 flex items-center justify-center shrink-0">
              <Plus className="w-4 h-4 text-blue-600 dark:text-blue-400" />
            </div>
            <div className="flex-1">
              <Type className="text-sm font-medium no-underline">
                Add more sources
              </Type>
            </div>
            <ArrowRight className="w-4 h-4 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
          </div>
        </routes.sources.Link>
        <routes.elements.Link
          className="no-underline hover:no-underline"
          queryParams={{ toolset: toolsetSlug }}
        >
          <div className="group flex items-center gap-3 p-3 rounded-lg border hover:border-foreground/20 hover:bg-muted/30 transition-all [&_*]:no-underline h-full">
            <div className="w-8 h-8 rounded-md bg-violet-500/10 dark:bg-violet-500/20 flex items-center justify-center shrink-0">
              <MessageCircle className="w-4 h-4 text-violet-600 dark:text-violet-400" />
            </div>
            <div className="flex-1">
              <Type className="text-sm font-medium no-underline">
                Deploy as chat
              </Type>
            </div>
            <ArrowRight className="w-4 h-4 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
          </div>
        </routes.elements.Link>
        <a
          href={`${getServerURL()}/mcp/${mcpSlug}/install`}
          target="_blank"
          rel="noopener noreferrer"
          className="no-underline hover:no-underline"
        >
          <div className="group flex items-center gap-3 p-3 rounded-lg border hover:border-foreground/20 hover:bg-muted/30 transition-all [&_*]:no-underline h-full">
            <div className="w-8 h-8 rounded-md bg-emerald-500/10 dark:bg-emerald-500/20 flex items-center justify-center shrink-0">
              <Plug className="w-4 h-4 text-emerald-600 dark:text-emerald-400" />
            </div>
            <div className="flex-1">
              <Type className="text-sm font-medium no-underline">
                Connect via Claude, Cursor
              </Type>
            </div>
            <ArrowRight className="w-4 h-4 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
          </div>
        </a>
        <routes.mcp.details.Link
          params={[toolsetSlug]}
          className="no-underline hover:no-underline"
        >
          <div className="group flex items-center gap-3 p-3 rounded-lg border hover:border-foreground/20 hover:bg-muted/30 transition-all [&_*]:no-underline h-full">
            <div className="w-8 h-8 rounded-md bg-orange-500/10 dark:bg-orange-500/20 flex items-center justify-center shrink-0">
              <Settings className="w-4 h-4 text-orange-600 dark:text-orange-400" />
            </div>
            <div className="flex-1">
              <Type className="text-sm font-medium no-underline">
                Configure MCP settings
              </Type>
            </div>
            <ArrowRight className="w-4 h-4 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
          </div>
        </routes.mcp.details.Link>
      </div>
    </div>
  );
}

// --- Error Phase ---

function ErrorPhase({
  releaseState,
  onClose,
}: {
  releaseState: ExternalMcpReleaseState;
  onClose: () => void;
}) {
  const routes = useTargetRoutes(releaseState);

  return (
    <div className="py-2 space-y-4">
      <div className="flex items-start gap-3 p-3 rounded-lg border border-destructive/30 bg-destructive/5">
        <AlertCircle className="w-5 h-5 text-destructive shrink-0 mt-0.5" />
        <div className="flex-1">
          <Type className="font-medium text-destructive">
            Deployment failed
          </Type>
          <Type small className="text-destructive/80 mt-1">
            {releaseState.error ?? "An unexpected error occurred."}
          </Type>
        </div>
      </div>
      {releaseState.deploymentLogs.length > 0 && (
        <div className="rounded-lg border bg-muted/30 p-3 max-h-48 overflow-y-auto font-mono text-xs space-y-1">
          {releaseState.deploymentLogs.map((log) => (
            <div
              key={log.id}
              className={log.event.includes("error") ? "text-destructive" : ""}
            >
              {log.message}
            </div>
          ))}
        </div>
      )}
      <Dialog.Footer>
        {releaseState.deploymentId && (
          <routes.deployments.deployment.Link
            params={[releaseState.deploymentId]}
            className="no-underline hover:no-underline"
          >
            <Button variant="secondary">
              <ExternalLink className="w-4 h-4" />
              <Button.Text>View Deployment</Button.Text>
            </Button>
          </routes.deployments.deployment.Link>
        )}
        <Button variant="tertiary" onClick={onClose}>
          <Button.Text>Close</Button.Text>
        </Button>
      </Dialog.Footer>
    </div>
  );
}
