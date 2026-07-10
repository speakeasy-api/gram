import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { InlineEmptyState } from "@/components/ui/inline-empty-state";
import { useTelemetry } from "@/contexts/Telemetry";
import {
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from "@/components/ui/moonshine";
import { ChevronDown, Database, Plus } from "lucide-react";
import { AddSourceMenuItems } from "./AddSourceMenuItems";

type SourcesEmptyStateProps = {
  isTunneledMcpEnabled: boolean;
};

function sourcesEmptyStateDescription(
  isFunctionsEnabled: boolean,
  isTunneledMcpEnabled: boolean,
): string {
  if (isFunctionsEnabled && isTunneledMcpEnabled) {
    return "OpenAPI documents, functions, remote MCP servers, tunneled MCP servers, and third-party MCP servers providing tools for your project";
  }
  if (isFunctionsEnabled) {
    return "OpenAPI documents, functions, remote MCP servers, and third-party MCP servers providing tools for your project";
  }
  if (isTunneledMcpEnabled) {
    return "OpenAPI documents, remote MCP servers, tunneled MCP servers, and third-party MCP servers providing tools for your project";
  }
  return "OpenAPI documents, remote MCP servers, and third-party MCP servers providing tools for your project";
}

function sourcesEmptyStateBody(
  isFunctionsEnabled: boolean,
  isTunneledMcpEnabled: boolean,
): string {
  if (isFunctionsEnabled && isTunneledMcpEnabled) {
    return "Add an OpenAPI spec, custom function, third-party server, remote server, or private server tunnel to generate tools for your MCP server.";
  }
  if (isFunctionsEnabled) {
    return "Add an OpenAPI spec, custom function, third-party server, or remote server to generate tools for your MCP server.";
  }
  if (isTunneledMcpEnabled) {
    return "Add an OpenAPI spec, third-party server, remote server, or private server tunnel to generate tools for your MCP server.";
  }
  return "Add an OpenAPI spec, third-party server, or remote server to generate tools for your MCP server.";
}

export function SourcesEmptyState({
  isTunneledMcpEnabled,
}: SourcesEmptyStateProps): JSX.Element {
  const telemetry = useTelemetry();
  const isFunctionsEnabled =
    telemetry.isFeatureEnabled("gram-functions") ?? false;

  return (
    <Page.Section>
      <Page.Section.Title>Sources</Page.Section.Title>
      <Page.Section.Description className="max-w-2xl">
        {sourcesEmptyStateDescription(isFunctionsEnabled, isTunneledMcpEnabled)}
      </Page.Section.Description>
      <Page.Section.Body>
        <InlineEmptyState
          className="py-16"
          icon={<Database />}
          title="No sources yet"
          description={sourcesEmptyStateBody(
            isFunctionsEnabled,
            isTunneledMcpEnabled,
          )}
          action={
            <RequireScope scope="project:write" level="component">
              {({ disabled }) => (
                <DropdownMenu>
                  <DropdownMenuTrigger asChild disabled={disabled}>
                    <Button>
                      <Button.LeftIcon>
                        <Plus className="h-4 w-4" />
                      </Button.LeftIcon>
                      <Button.Text>Add Source</Button.Text>
                      <ChevronDown className="ml-1 h-4 w-4" />
                    </Button>
                  </DropdownMenuTrigger>
                  {!disabled && (
                    <DropdownMenuContent
                      align="center"
                      className="w-[320px] p-1"
                    >
                      <AddSourceMenuItems
                        isFunctionsEnabled={isFunctionsEnabled}
                        isTunneledMcpEnabled={isTunneledMcpEnabled}
                      />
                    </DropdownMenuContent>
                  )}
                </DropdownMenu>
              )}
            </RequireScope>
          }
        />
      </Page.Section.Body>
    </Page.Section>
  );
}
