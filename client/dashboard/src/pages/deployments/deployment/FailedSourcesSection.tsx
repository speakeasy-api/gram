import { RequireScope } from "@/components/require-scope";
import { Checkbox } from "@/components/ui/checkbox";
import type { FailedSource } from "@/components/sources/useFailedDeploymentSources";
import { cn } from "@/lib/utils";
import { useSdkClient } from "@/contexts/Sdk";
import type {
  Deployment,
  DeploymentLogEvent,
} from "@gram/client/models/components";
import { Alert, Badge, Button, Dialog } from "@speakeasy-api/moonshine";
import {
  ChevronDown,
  ChevronRight,
  CircleAlert,
  Code,
  FileCode,
  Loader2,
  Server,
  Wrench,
} from "lucide-react";
import { useRoutes } from "@/routes";
import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";

interface FailedSourcesSectionProps {
  failedSources: FailedSource[];
  generalErrors: DeploymentLogEvent[];
  deployment: Deployment;
  onRemoveSuccess: () => void;
}

const SOURCE_ICONS = {
  openapi: FileCode,
  function: Code,
  externalmcp: Server,
} as const;

export function FailedSourcesSection({
  failedSources,
  generalErrors,
  deployment,
  onRemoveSuccess,
}: FailedSourcesSectionProps) {
  const client = useSdkClient();
  const routes = useRoutes();
  // Auto-select only sources with no toolset references
  const [selected, setSelected] = useState<Set<string>>(
    () =>
      new Set(failedSources.filter((s) => s.toolCount === 0).map((s) => s.id)),
  );
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  const [pending, setPending] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);

  // Reset selection when sources change
  useEffect(() => {
    setSelected(
      new Set(failedSources.filter((s) => s.toolCount === 0).map((s) => s.id)),
    );
    setExpanded(new Set());
    setPending(false);
  }, [failedSources]);

  const toggleSelected = useCallback((id: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  }, []);

  const toggleExpanded = useCallback((id: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  }, []);

  const selectedSources = useMemo(
    () => failedSources.filter((s) => selected.has(s.id)),
    [failedSources, selected],
  );

  const selectedWithTools = useMemo(
    () => selectedSources.filter((s) => s.toolCount > 0),
    [selectedSources],
  );

  const handleRemoveClick = () => {
    if (selectedSources.length === 0) return;
    if (selectedWithTools.length > 0) {
      setConfirmOpen(true);
    } else {
      doRemove();
    }
  };

  const doRemove = async () => {
    setConfirmOpen(false);
    if (selectedSources.length === 0) return;

    setPending(true);
    const toastId = "remove-failed-sources";
    toast.loading("Removing failed sources...", { id: toastId });

    try {
      const excludeOpenapiv3Assets: string[] = [];
      const excludeFunctions: string[] = [];
      const excludeExternalMcps: string[] = [];

      for (const source of selectedSources) {
        switch (source.type) {
          case "openapi":
            excludeOpenapiv3Assets.push(source.id);
            break;
          case "function":
            excludeFunctions.push(source.id);
            break;
          case "externalmcp":
            excludeExternalMcps.push(source.slug);
            break;
        }
      }

      await client.deployments.evolveDeployment({
        evolveForm: {
          deploymentId: deployment.id,
          nonBlocking: true,
          ...(excludeOpenapiv3Assets.length > 0 && {
            excludeOpenapiv3Assets,
          }),
          ...(excludeFunctions.length > 0 && { excludeFunctions }),
          ...(excludeExternalMcps.length > 0 && { excludeExternalMcps }),
        },
      });

      toast.success(
        `Removed ${selectedSources.length} failed source${selectedSources.length !== 1 ? "s" : ""}`,
        { id: toastId },
      );
      onRemoveSuccess();
      routes.deployments.goTo();
    } catch (error) {
      console.error("Failed to remove sources:", error);
      toast.error("Failed to remove sources. Please try again.", {
        id: toastId,
      });
    } finally {
      setPending(false);
    }
  };

  const totalAffectedTools = selectedWithTools.reduce(
    (sum, s) => sum + s.toolCount,
    0,
  );

  return (
    <>
      <section className="border-destructive/40 bg-destructive/5 space-y-3 rounded-lg border p-4">
        <div className="flex items-center gap-2">
          <CircleAlert className="text-destructive size-5 shrink-0" />
          <h3 className="text-sm font-semibold">
            {failedSources.length > 0
              ? `${failedSources.length} source${failedSources.length !== 1 ? "s" : ""} failed`
              : "Deployment failed"}
          </h3>
        </div>

        {failedSources.length > 0 && (
          <p className="text-muted-foreground text-sm">
            Select the sources to remove and redeploy without them.
          </p>
        )}

        <div className="max-h-80 space-y-2 overflow-y-auto">
          {failedSources.map((source) => {
            const IconComponent = SOURCE_ICONS[source.type];
            const isSelected = selected.has(source.id);
            const isExpanded = expanded.has(source.id);

            return (
              <div
                key={source.id}
                className={cn(
                  "rounded-lg border p-3 transition-colors",
                  isSelected
                    ? "border-destructive/40 bg-destructive/5"
                    : "border-border bg-card",
                )}
              >
                <div className="flex items-center gap-3">
                  <Checkbox
                    checked={isSelected}
                    onCheckedChange={() => toggleSelected(source.id)}
                    disabled={pending}
                  />
                  <div className="bg-destructive/10 flex h-8 w-8 shrink-0 items-center justify-center rounded-md">
                    <IconComponent className="text-destructive h-4 w-4" />
                  </div>
                  <div className="min-w-0 flex-1">
                    <span className="block truncate text-sm font-medium">
                      {source.name}
                    </span>
                    <span className="text-muted-foreground text-xs">
                      {source.type === "openapi"
                        ? "API Source"
                        : source.type === "function"
                          ? "Function"
                          : "External MCP"}{" "}
                      &middot;{" "}
                      {`${source.errors.length} error${source.errors.length !== 1 ? "s" : ""}`}
                    </span>
                  </div>
                  {source.toolCount > 0 && (
                    <Badge
                      variant="neutral"
                      className="flex shrink-0 items-center gap-1"
                    >
                      <Wrench className="size-3" />
                      {`${source.toolCount} ${source.toolCount === 1 ? "tool" : "tools"}`}
                    </Badge>
                  )}
                  {source.errors.length > 0 && (
                    <button
                      type="button"
                      onClick={() => toggleExpanded(source.id)}
                      className="hover:bg-muted text-muted-foreground rounded p-1 transition-colors"
                    >
                      {isExpanded ? (
                        <ChevronDown className="size-4" />
                      ) : (
                        <ChevronRight className="size-4" />
                      )}
                    </button>
                  )}
                </div>
                {isExpanded && source.errors.length > 0 && (
                  <div className="mt-2 ml-11 space-y-1">
                    {source.errors.map((err) => (
                      <div
                        key={err.id}
                        className="text-destructive bg-destructive/5 rounded px-2 py-1.5 font-mono text-xs break-all"
                      >
                        {err.message}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            );
          })}

          {generalErrors.length > 0 && (
            <div className="border-border bg-card rounded-lg border p-3">
              <span className="text-sm font-medium">General errors</span>
              <div className="mt-2 space-y-1">
                {generalErrors.map((err) => (
                  <div
                    key={err.id}
                    className="text-destructive bg-destructive/5 rounded px-2 py-1.5 font-mono text-xs break-all"
                  >
                    {err.message}
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>

        {failedSources.length > 0 && (
          <div className="flex justify-end">
            <RequireScope scope="project:write" level="component">
              <Button
                variant="destructive-primary"
                onClick={handleRemoveClick}
                disabled={pending || selected.size === 0}
              >
                {pending ? (
                  <>
                    <Button.LeftIcon>
                      <Loader2 className="size-4 animate-spin" />
                    </Button.LeftIcon>
                    <Button.Text>Removing...</Button.Text>
                  </>
                ) : (
                  <Button.Text>
                    {`Remove ${selected.size > 0 ? selected.size : ""} source${selected.size !== 1 ? "s" : ""}`}
                  </Button.Text>
                )}
              </Button>
            </RequireScope>
          </div>
        )}
      </section>

      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <Dialog.Content className="max-w-lg!">
          <Dialog.Header>
            <Dialog.Title>Active MCP servers affected</Dialog.Title>
            <Dialog.Description>
              {`${selectedWithTools.length} of the selected source${selectedWithTools.length !== 1 ? "s have" : " has"} ${totalAffectedTools} tool${totalAffectedTools !== 1 ? "s" : ""} referenced by active MCP servers.`}
            </Dialog.Description>
          </Dialog.Header>
          <Alert variant="warning" dismissible={false}>
            Removing these sources will break MCP servers that depend on their
            tools. You may need to update affected MCP servers afterward.
          </Alert>
          <ul className="space-y-1 text-sm">
            {selectedWithTools.map((s) => (
              <li key={s.id} className="flex items-center gap-2">
                <Wrench className="text-muted-foreground size-3 shrink-0" />
                <span className="font-medium">{s.name}</span>
                <span className="text-muted-foreground">
                  {`${s.toolCount} ${s.toolCount === 1 ? "tool" : "tools"}`}
                </span>
              </li>
            ))}
          </ul>
          <Dialog.Footer>
            <Button variant="tertiary" onClick={() => setConfirmOpen(false)}>
              Cancel
            </Button>
            <Button variant="destructive-primary" onClick={doRemove}>
              <Button.Text>Remove anyway</Button.Text>
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
