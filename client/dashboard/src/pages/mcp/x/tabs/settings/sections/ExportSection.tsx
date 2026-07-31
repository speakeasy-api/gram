import { Text } from "@/components/ui/Text";
import { useTelemetry } from "@/contexts/Telemetry";
import type { McpEndpoint } from "@gram/client/models/components/mcpendpoint.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useExportMcpMetadataMutation } from "@gram/client/react-query/exportMcpMetadata.js";
import { Button } from "@/components/ui/Button";
import { Download } from "lucide-react";
import { toast } from "sonner";
import { SettingsSection } from "../SettingsSection";

// The export endpoint is keyed by the public MCP slug, so prefer the
// platform-hosted endpoint whose slug matches the historical export key.
function exportSlugFromEndpoints(endpoints: McpEndpoint[]): string | undefined {
  const platformEndpoint = endpoints.find((e) => !e.customDomainId);
  return (platformEndpoint ?? endpoints[0])?.slug ?? undefined;
}

export function ExportSection({
  mcpServer,
  endpoints,
}: {
  mcpServer: McpServer;
  endpoints: McpEndpoint[];
}): JSX.Element {
  const telemetry = useTelemetry();
  const exportMutation = useExportMcpMetadataMutation();

  const exportSlug = exportSlugFromEndpoints(endpoints);
  const exportUnavailable = mcpServer.visibility === "disabled" || !exportSlug;

  const handleExportJson = async () => {
    if (!exportSlug) {
      toast.error("MCP server has no endpoint to export");
      return;
    }

    const toastId = toast.loading("Exporting MCP configuration...");

    try {
      const result = await exportMutation.mutateAsync({
        request: {
          exportMcpMetadataRequestBody: {
            mcpSlug: exportSlug,
          },
        },
      });

      const blob = new Blob([JSON.stringify(result, null, 2)], {
        type: "application/json",
      });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `${exportSlug}-mcp-config.json`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);

      telemetry.capture("mcp_event", {
        action: "mcp_json_exported",
        slug: exportSlug,
      });

      toast.success("MCP configuration exported", { id: toastId });
    } catch (error) {
      console.error("Failed to export MCP configuration:", error);
      toast.error(
        `Failed to export: ${error instanceof Error ? error.message : "Unknown error"}`,
        {
          id: toastId,
        },
      );
    }
  };

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Export</SettingsSection.Title>
        <SettingsSection.Description>
          Export your MCP server configuration as JSON.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <div className="flex items-center gap-3">
            <Button
              variant="secondary"
              size="md"
              onClick={() => void handleExportJson()}
              disabled={exportUnavailable || exportMutation.isPending}
            >
              <Button.LeftIcon>
                <Download className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>Export JSON</Button.Text>
            </Button>
            {exportUnavailable && (
              <Text muted small>
                Enable this server and add an endpoint to export its
                configuration.
              </Text>
            )}
          </div>
        </SettingsSection.Body>
      </SettingsSection.Panel>
    </SettingsSection>
  );
}
