import { Card } from "@/components/ui/card";
import { Type } from "@/components/ui/type";
import { Toolset } from "@/lib/toolTypes";
import { Stack } from "@speakeasy-api/moonshine";
import { Server } from "lucide-react";

interface ServerTabContentProps {
  toolset: Toolset;
}

export function ServerTabContent({ toolset }: ServerTabContentProps) {
  // Find the external MCP tool to display its metadata
  const externalMcpTool = toolset.tools.find((t) => t.type === "external-mcp");

  if (!externalMcpTool || externalMcpTool.type !== "external-mcp") {
    return (
      <div className="text-muted-foreground">
        No external MCP server configured.
      </div>
    );
  }

  return (
    <Stack direction="vertical" gap={6}>
      <Card>
        <Card.Title>
          <Stack direction="horizontal" gap={3} align="center">
            <div className="w-10 h-10 rounded-lg bg-primary/10 flex items-center justify-center">
              <Server className="w-5 h-5 text-primary" />
            </div>
            <Stack gap={1}>
              <Type variant="subheading">External MCP Server</Type>
              <Type small muted>
                {externalMcpTool.slug}
              </Type>
            </Stack>
          </Stack>
        </Card.Title>
        <Card.Description>
          <Stack direction="vertical" gap={4} className="mt-4">
            <div>
              <Type small muted className="block mb-1">
                Remote URL
              </Type>
              <Type className="font-mono text-sm">
                {externalMcpTool.remoteUrl}
              </Type>
            </div>
            {externalMcpTool.requiresOauth && (
              <div>
                <Type small muted className="block mb-1">
                  Authentication
                </Type>
                <Type>OAuth required</Type>
              </div>
            )}
          </Stack>
        </Card.Description>
      </Card>
    </Stack>
  );
}
