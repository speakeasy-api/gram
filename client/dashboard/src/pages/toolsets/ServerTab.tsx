import { Card } from "@/components/ui/Card";
import { Text } from "@/components/ui/Text";
import { Toolset } from "@/lib/toolTypes";
import { Stack } from "@/components/ui/Stack";
import { Server } from "lucide-react";

interface ServerTabContentProps {
  toolset: Toolset;
}

export function ServerTabContent({
  toolset,
}: ServerTabContentProps): JSX.Element {
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
            <div className="bg-primary/10 flex h-10 w-10 items-center justify-center">
              <Server className="text-primary h-5 w-5" />
            </div>
            <Stack gap={1}>
              <Text variant="subheading">External MCP Server</Text>
              <Text small muted>
                {tool.slug}
              </Text>
            </Stack>
          </Stack>
        </Card.Title>
        <Card.Description>
          <Stack direction="vertical" gap={4} className="mt-4">
            <div>
              <Text small muted className="mb-1 block">
                Remote URL
              </Text>
              <Text className="font-mono text-sm">{tool.remoteUrl}</Text>
            </div>
            {tool.requiresOauth && (
              <div>
                <Text small muted className="mb-1 block">
                  Authentication
                </Text>
                <Text>OAuth required</Text>
              </div>
            )}
          </Stack>
        </Card.Description>
      </Card>
    </Stack>
  );
}
