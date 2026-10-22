import { Button } from "@/components/ui/Button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { useTelemetry } from "@/contexts/Telemetry";
import { PageSection } from "@/pages/mcp/PageSection";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import { useExportMcpMetadataMutation } from "@gram/client/react-query/exportMcpMetadata.js";
import { Download } from "lucide-react";
import type React from "react";
import { toast } from "sonner";

/** Downloads a hosted server's MCP configuration, keyed by its server id. */
export function ExportJsonSection({
  mcpServer,
}: {
  mcpServer: McpServer;
}): React.JSX.Element {
  const telemetry = useTelemetry();
  const exportMutation = useExportMcpMetadataMutation();
  const disabled = mcpServer.visibility === "disabled";

  const handleExport = async () => {
    const toastId = toast.loading("Exporting MCP configuration...");
    try {
      const result = await exportMutation.mutateAsync({
        request: {
          exportMcpMetadataRequestBody: { mcpServerId: mcpServer.id },
        },
      });
      const blob = new Blob([JSON.stringify(result, null, 2)], {
        type: "application/json",
      });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = `${mcpServer.slug ?? mcpServer.id}-mcp-config.json`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
      telemetry.capture("mcp_event", {
        action: "mcp_json_exported",
        mcp_server_id: mcpServer.id,
      });
      toast.success("MCP configuration exported", { id: toastId });
    } catch (error) {
      toast.error(
        `Failed to export: ${error instanceof Error ? error.message : "Unknown error"}`,
        { id: toastId },
      );
    }
  };

  return (
    <PageSection
      heading="Export"
      description="Export your MCP server configuration."
    >
      <div>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="secondary"
              size="md"
              onClick={() => void handleExport()}
              disabled={disabled || exportMutation.isPending}
            >
              <Button.LeftIcon>
                <Download className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>Export JSON</Button.Text>
            </Button>
          </TooltipTrigger>
          {disabled && (
            <TooltipContent>
              Enable the server to export its configuration
            </TooltipContent>
          )}
        </Tooltip>
      </div>
    </PageSection>
  );
}
