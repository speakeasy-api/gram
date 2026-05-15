import { useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Stack } from "@speakeasy-api/moonshine";

import { Heading } from "@/components/ui/heading";
import { Switch } from "@/components/ui/switch";
import { Type } from "@/components/ui/type";
import {
  invalidateAllToolset,
  useUpdateToolsetMutation,
} from "@gram/client/react-query";

import { useTelemetry } from "@/contexts/Telemetry";
import { useLatestDeployment } from "@/hooks/toolTypes";
import { useRBAC } from "@/hooks/useRBAC";
import type { Toolset } from "@/lib/toolTypes";

import {
  applyFunctionToggle,
  extractFunctionSubscriptions,
} from "./autoSyncSources";

interface AutoSyncSourcesCardProps {
  toolset: Toolset;
}

export function AutoSyncSourcesCard({ toolset }: AutoSyncSourcesCardProps) {
  const { hasScope } = useRBAC();
  const canWrite = hasScope("mcp:write");
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();

  const { data: deploymentResult } = useLatestDeployment();
  const functionSources = deploymentResult?.deployment?.functionsAssets ?? [];

  const subscribed = useMemo(
    () => extractFunctionSubscriptions(toolset.autoSyncSources ?? []),
    [toolset.autoSyncSources],
  );

  const updateToolsetMutation = useUpdateToolsetMutation();

  const toggle = (slug: string, next: boolean) => {
    const nextSources = applyFunctionToggle(
      toolset.autoSyncSources ?? [],
      slug,
      next,
    );

    updateToolsetMutation.mutate(
      {
        request: {
          slug: toolset.slug,
          updateToolsetRequestBody: { autoSyncSources: nextSources },
        },
      },
      {
        onSuccess: () => {
          invalidateAllToolset(queryClient);
          telemetry.capture("mcp_event", {
            action: next
              ? "auto_sync_source_added"
              : "auto_sync_source_removed",
            slug: toolset.slug,
            source: slug,
          });
        },
        onError: (error) => {
          toast.error(
            `Failed to update auto-sync sources: ${
              error instanceof Error ? error.message : "unknown error"
            }`,
          );
        },
      },
    );
  };

  return (
    <Stack gap={2} className="mb-8">
      <Heading variant="h3">Auto-sync sources</Heading>
      <Type muted small className="max-w-2xl">
        Toolsets follow named function sources. When you push a new deployment,
        new tool URNs from these sources are added to this MCP automatically.
        Existing tools always update in place.
      </Type>
      {functionSources.length === 0 ? (
        <Type small muted italic>
          No function sources in this project yet. Push a function deployment to
          make sources available here.
        </Type>
      ) : (
        <Stack gap={1}>
          {functionSources.map((source) => (
            <AutoSyncRow
              key={source.slug}
              name={source.name}
              slug={source.slug}
              checked={subscribed.has(source.slug)}
              disabled={!canWrite || updateToolsetMutation.isPending}
              onToggle={(next) => toggle(source.slug, next)}
            />
          ))}
        </Stack>
      )}
    </Stack>
  );
}

interface AutoSyncRowProps {
  name: string;
  slug: string;
  checked: boolean;
  disabled: boolean;
  onToggle: (next: boolean) => void;
}

function AutoSyncRow({
  name,
  slug,
  checked,
  disabled,
  onToggle,
}: AutoSyncRowProps) {
  const labelId = `auto-sync-row-${slug}`;
  return (
    <Stack
      direction="horizontal"
      align="center"
      gap={2}
      className="border-border bg-card rounded-md border px-3 py-2"
    >
      <Stack gap={0} className="min-w-0 flex-1">
        <Type variant="small" className="truncate" id={labelId}>
          {name}
        </Type>
        <Type mono variant="small" muted className="truncate">
          {slug}
        </Type>
      </Stack>
      <Switch
        checked={checked}
        onCheckedChange={onToggle}
        disabled={disabled}
        aria-labelledby={labelId}
      />
    </Stack>
  );
}
