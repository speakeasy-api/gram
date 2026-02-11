import { Checkbox } from "@/components/ui/checkbox";
import { cn } from "@/lib/utils";
import { useSdkClient } from "@/contexts/Sdk";
import type {
  Deployment,
  DeploymentLogEvent,
} from "@gram/client/models/components";
import { Button, Dialog } from "@speakeasy-api/moonshine";
import {
  ChevronDown,
  ChevronRight,
  Code,
  FileCode,
  Loader2,
  Server,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import type { FailedSource } from "./useFailedDeploymentSources";

interface FailedDeploymentModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  failedSources: FailedSource[];
  generalErrors: DeploymentLogEvent[];
  deployment: Deployment;
  onRedeploySuccess: () => void;
}

const SOURCE_ICONS = {
  openapi: FileCode,
  function: Code,
  externalmcp: Server,
} as const;

export function FailedDeploymentModal({
  open,
  onOpenChange,
  failedSources,
  generalErrors,
  deployment,
  onRedeploySuccess,
}: FailedDeploymentModalProps) {
  const client = useSdkClient();
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  const [pending, setPending] = useState(false);

  // Reset selection when modal opens with new data
  useEffect(() => {
    if (open) {
      setSelected(new Set(failedSources.map((s) => s.id)));
      setExpanded(new Set());
      setPending(false);
    }
  }, [open, failedSources]);

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

  const handleRemoveAndRedeploy = async () => {
    if (selectedSources.length === 0) return;

    setPending(true);
    const toastId = "redeploy-without-failed";
    toast.loading("Redeploying without failed sources...", { id: toastId });

    try {
      // Group selected sources by type to build exclude lists
      const excludeOpenapiv3Assets: string[] = [];
      const excludeFunctions: string[] = [];
      const excludeExternalMcps: string[] = [];

      for (const source of selectedSources) {
        switch (source.type) {
          case "openapi":
            excludeOpenapiv3Assets.push(source.slug);
            break;
          case "function":
            excludeFunctions.push(source.slug);
            break;
          case "externalmcp":
            excludeExternalMcps.push(source.slug);
            break;
        }
      }

      await client.deployments.evolveDeployment({
        evolveForm: {
          deploymentId: deployment.id,
          ...(excludeOpenapiv3Assets.length > 0 && {
            excludeOpenapiv3Assets,
          }),
          ...(excludeFunctions.length > 0 && { excludeFunctions }),
          ...(excludeExternalMcps.length > 0 && { excludeExternalMcps }),
        },
      });

      toast.success(
        `Redeployed without ${selectedSources.length} failed source${selectedSources.length !== 1 ? "s" : ""}`,
        { id: toastId },
      );
      onRedeploySuccess();
    } catch (error) {
      console.error("Failed to redeploy:", error);
      toast.error("Failed to redeploy. Please try again.", { id: toastId });
    } finally {
      setPending(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="max-w-2xl!">
        <Dialog.Header>
          <Dialog.Title>Deployment Failed</Dialog.Title>
          <Dialog.Description>
            {failedSources.length > 0
              ? `${failedSources.length} source${failedSources.length !== 1 ? "s" : ""} caused errors in the latest deployment. Select the ones to remove and redeploy without them.`
              : "The latest deployment failed. Check the error details below."}
          </Dialog.Description>
        </Dialog.Header>

        <div className="max-h-80 overflow-y-auto space-y-2">
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
                    : "border-border",
                )}
              >
                <div className="flex items-center gap-3">
                  <Checkbox
                    checked={isSelected}
                    onCheckedChange={() => toggleSelected(source.id)}
                    disabled={pending}
                  />
                  <div className="w-8 h-8 rounded-md bg-destructive/10 flex items-center justify-center shrink-0">
                    <IconComponent className="w-4 h-4 text-destructive" />
                  </div>
                  <div className="flex-1 min-w-0">
                    <span className="text-sm font-medium truncate block">
                      {source.name}
                    </span>
                    <span className="text-xs text-muted-foreground">
                      {source.type === "openapi"
                        ? "API Source"
                        : source.type === "function"
                          ? "Function"
                          : "External MCP"}{" "}
                      &middot; {source.errors.length} error
                      {source.errors.length !== 1 ? "s" : ""}
                    </span>
                  </div>
                  {source.errors.length > 0 && (
                    <button
                      type="button"
                      onClick={() => toggleExpanded(source.id)}
                      className="p-1 rounded hover:bg-muted transition-colors text-muted-foreground"
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
                        className="text-xs text-destructive bg-destructive/5 rounded px-2 py-1.5 font-mono break-all"
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
            <div className="rounded-lg border border-border p-3">
              <span className="text-sm font-medium">General errors</span>
              <div className="mt-2 space-y-1">
                {generalErrors.map((err) => (
                  <div
                    key={err.id}
                    className="text-xs text-destructive bg-destructive/5 rounded px-2 py-1.5 font-mono break-all"
                  >
                    {err.message}
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>

        <Dialog.Footer>
          <Button
            variant="tertiary"
            onClick={() => onOpenChange(false)}
            disabled={pending}
          >
            Cancel
          </Button>
          {failedSources.length > 0 && (
            <Button
              variant="destructive-primary"
              onClick={handleRemoveAndRedeploy}
              disabled={pending || selected.size === 0}
            >
              {pending ? (
                <>
                  <Button.LeftIcon>
                    <Loader2 className="size-4 animate-spin" />
                  </Button.LeftIcon>
                  <Button.Text>Redeploying...</Button.Text>
                </>
              ) : (
                <Button.Text>
                  Remove {selected.size > 0 ? selected.size : ""} &amp;
                  Redeploy
                </Button.Text>
              )}
            </Button>
          )}
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
