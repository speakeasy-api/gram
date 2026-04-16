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
  const externalMcpTool = toolset.rawTools.find(
    (t) => t.externalMcpToolDefinition !== undefined,
  );

  if (
    !externalMcpTool ||
    externalMcpTool.externalMcpToolDefinition === undefined
  ) {
    return (
      <div className="text-muted-foreground">
        No external MCP server configured.
      </div>
    );
  }

  const tool = externalMcpTool.externalMcpToolDefinition;

  return (
    <Stack direction="vertical" gap={6}>
      <Card>
        <Card.Title>
          <Stack direction="horizontal" gap={3} align="center">
            <div className="bg-primary/10 flex h-10 w-10 items-center justify-center rounded-lg">
              <Server className="text-primary h-5 w-5" />
            </div>
            <Stack gap={1}>
              <Type variant="subheading">External MCP Server</Type>
              <Type small muted>
                {tool.slug}
              </Type>
            </Stack>
          </Stack>
        </Card.Title>
        <Card.Description>
          <Stack direction="vertical" gap={4} className="mt-4">
            <div>
              <Type small muted className="mb-1 block">
                Remote URL
              </Type>
              <Type className="font-mono text-sm">{tool.remoteUrl}</Type>
            </div>
            {tool.requiresOauth && (
              <div>
                <Type small muted className="mb-1 block">
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
