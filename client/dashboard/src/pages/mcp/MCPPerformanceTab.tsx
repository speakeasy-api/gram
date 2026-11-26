import { SettingsSection } from "@/components/detail/settings-section";
import { TechnicalDetails } from "@/components/technical-details";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRBAC } from "@/hooks/useRBAC";
import { RadioRows } from "@/lib/remote-identity/components/RadioRows";
import type { Toolset } from "@/lib/toolTypes";
import { invalidateAllToolset } from "@gram/client/react-query/toolset.js";
import { useUpdateToolsetMutation } from "@gram/client/react-query/updateToolset.js";
import { useQueryClient } from "@tanstack/react-query";
import { ExternalLink } from "lucide-react";
import { toast } from "sonner";
import * as authz from "@/lib/authz.gen";

/**
 * How agents find this server's tools. Gateways have the same setting under
 * the same name, with the modes a gateway supports.
 */
export function MCPPerformanceTab({
  toolset,
}: {
  toolset: Toolset;
}): JSX.Element {
  const { can } = useRBAC();
  const canWrite = can(authz.mcpServer(toolset.id, toolset.projectId).write());
  const queryClient = useQueryClient();
  const telemetry = useTelemetry();

  const updateToolsetMutation = useUpdateToolsetMutation({
    onSuccess: () => {
      void invalidateAllToolset(queryClient);
      toast.success("Tool discovery updated");
      telemetry.capture("mcp_event", {
        action: "tool_selection_mode_changed",
        slug: toolset.slug,
      });
    },
    onError: () => {
      toast.error("Failed to update tool discovery");
    },
  });

  const toolSelectionMode = toolset.toolSelectionMode ?? "static";

  const onSelectMode = (mode: string) => {
    if (!canWrite || mode === toolSelectionMode) return;
    updateToolsetMutation.mutate({
      request: {
        slug: toolset.slug,
        updateToolsetRequestBody: { toolSelectionMode: mode },
      },
    });
  };

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Tool discovery</SettingsSection.Title>
        <SettingsSection.Description>
          How agents find this server&apos;s tools. Changes apply to new
          connections.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <RadioRows
            label="Tool discovery"
            value={toolSelectionMode}
            onValueChange={onSelectMode}
            disabled={!canWrite || updateToolsetMutation.isPending}
            rows={[
              {
                value: "static",
                title: "Static",
                description:
                  "Agents see every tool when they connect. Best for small servers.",
              },
              {
                value: "dynamic",
                title: "Dynamic",
                description:
                  "Agents search for the tools they need. Best for large servers; uses more tool calls.",
              },
            ]}
          />
          <TechnicalDetails>
            Static loads every tool schema into the model&apos;s context, so
            token use grows with tool count; a large server can use 200k+
            tokens. Dynamic runs search, describe, then execute, which can cut
            input tokens by up to 96% but takes 2–3 times as many tool calls.
          </TechnicalDetails>
          <a
            href="https://www.speakeasy.com/docs/mcp/build/toolsets/dynamic-toolsets"
            target="_blank"
            rel="noopener noreferrer"
            className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1.5 text-sm transition-colors"
          >
            Learn more about tool discovery
            <ExternalLink aria-hidden="true" className="h-3.5 w-3.5" />
          </a>
        </SettingsSection.Body>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
