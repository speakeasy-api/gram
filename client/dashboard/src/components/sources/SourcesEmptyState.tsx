import {
  ChevronDown,
  Code,
  Database,
  FileCode,
  Network,
  Plus,
  Server,
} from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";

import { Button } from "@/components/ui/Button";
import { Page } from "@/components/page-layout";
import { PlatformMcpPromotion } from "@/components/platform-mcp-cta";
import { RequireScope } from "@/components/require-scope";
import { Text } from "@/components/ui/Text";
import { useIsSpeakeasyStaff } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import { useSlugs } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";

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
  const routes = useRoutes();
  const { projectSlug } = useSlugs();
  const telemetry = useTelemetry();
  const isFunctionsEnabled =
    telemetry.isFeatureEnabled("gram-functions") ?? false;
  const isSpeakeasyStaff = useIsSpeakeasyStaff();

  return (
    <Page.Section>
      <Page.Section.Title>Sources</Page.Section.Title>
      <Page.Section.Description className="max-w-2xl">
        {sourcesEmptyStateDescription(isFunctionsEnabled, isTunneledMcpEnabled)}
      </Page.Section.Description>
      <Page.Section.Body>
        <div className="bg-muted/20 flex flex-col items-center justify-center border border-dashed px-8 py-16">
          <div className="border-border mb-4 flex h-12 w-12 items-center justify-center border">
            <Database className="text-muted-foreground h-6 w-6" />
          </div>
          <Text variant="subheading" className="mb-1">
            No sources yet
          </Text>
          <Text small muted className="mb-4 max-w-md text-center">
            {sourcesEmptyStateBody(isFunctionsEnabled, isTunneledMcpEnabled)}
          </Text>
          <RequireScope scope="project:write" level="component">
            {({ disabled }) => (
              <DropdownMenu>
                <DropdownMenuTrigger asChild disabled={disabled}>
                  <Button>
                    <Button.LeftIcon>
                      <Plus className="h-4 w-4" />
                    </Button.LeftIcon>
                    <Button.Text>Add Source</Button.Text>
                    <Button.RightIcon>
                      <ChevronDown className="h-4 w-4" />
                    </Button.RightIcon>
                  </Button>
                </DropdownMenuTrigger>
                {!disabled && (
                  <DropdownMenuContent align="center" className="w-[320px] p-1">
                    <DropdownMenuItem
                      onSelect={() => routes.sources.addOpenAPI.goTo()}
                      className="flex cursor-pointer items-start gap-3 p-2"
                    >
                      <div className="flex h-10 w-10 shrink-0 items-center justify-center bg-blue-500/10 dark:bg-blue-500/20">
                        <FileCode className="h-5 w-5 text-blue-600 dark:text-blue-400" />
                      </div>
                      <div className="flex flex-col gap-0.5">
                        <span className="font-medium">From your API</span>
                        <span className="text-muted-foreground text-xs">
                          Upload an OpenAPI spec to generate tools
                        </span>
                      </div>
                    </DropdownMenuItem>
                    {isFunctionsEnabled && (
                      <DropdownMenuItem
                        onSelect={() => routes.sources.addFunction.goTo()}
                        className="flex cursor-pointer items-start gap-3 p-2"
                      >
                        <div className="flex h-10 w-10 shrink-0 items-center justify-center bg-emerald-500/10 dark:bg-emerald-500/20">
                          <Code className="h-5 w-5 text-emerald-600 dark:text-emerald-400" />
                        </div>
                        <div className="flex flex-col gap-0.5">
                          <span className="font-medium">Write custom code</span>
                          <span className="text-muted-foreground text-xs">
                            Create tools with TypeScript functions
                          </span>
                        </div>
                      </DropdownMenuItem>
                    )}
                    <DropdownMenuItem
                      onSelect={() => routes.sources.addFromCatalog.goTo()}
                      className="flex cursor-pointer items-start gap-3 p-2"
                    >
                      <div className="flex h-10 w-10 shrink-0 items-center justify-center bg-violet-500/10 dark:bg-violet-500/20">
                        <Server className="h-5 w-5 text-violet-600 dark:text-violet-400" />
                      </div>
                      <div className="flex flex-col gap-0.5">
                        <span className="font-medium">3rd-party server</span>
                        <span className="text-muted-foreground text-xs">
                          Add pre-built servers from the catalog
                        </span>
                      </div>
                    </DropdownMenuItem>
                    <DropdownMenuItem
                      onSelect={() => routes.sources.addRemoteMcp.goTo()}
                      className="flex cursor-pointer items-start gap-3 p-2"
                    >
                      <div className="flex h-10 w-10 shrink-0 items-center justify-center bg-violet-500/10 dark:bg-violet-500/20">
                        <Network className="h-5 w-5 text-violet-600 dark:text-violet-400" />
                      </div>
                      <div className="flex flex-col gap-0.5">
                        <span className="font-medium">
                          Custom remote server
                        </span>
                        <span className="text-muted-foreground text-xs">
                          Add existing remote servers by URL
                        </span>
                      </div>
                    </DropdownMenuItem>
                    {isTunneledMcpEnabled && (
                      <DropdownMenuItem
                        onSelect={() => routes.sources.addTunneledMcp.goTo()}
                        className="flex cursor-pointer items-start gap-3 p-2"
                      >
                        <div className="flex h-10 w-10 shrink-0 items-center justify-center bg-cyan-500/10 dark:bg-cyan-500/20">
                          <Network className="h-5 w-5 text-cyan-700 dark:text-cyan-300" />
                        </div>
                        <div className="flex flex-col gap-0.5">
                          <span className="font-medium">
                            Tunneled MCP Server
                          </span>
                          <span className="text-muted-foreground text-xs">
                            Connect private MCP servers through a tunnel
                          </span>
                        </div>
                      </DropdownMenuItem>
                    )}
                    {isSpeakeasyStaff && (
                      <DropdownMenuItem
                        onSelect={() => routes.sources.addUnproxiedMcp.goTo()}
                        className="flex cursor-pointer items-start gap-3 p-2"
                      >
                        <div className="flex h-10 w-10 shrink-0 items-center justify-center bg-amber-500/10 dark:bg-amber-500/20">
                          <Server className="h-5 w-5 text-amber-600 dark:text-amber-400" />
                        </div>
                        <div className="flex flex-col gap-0.5">
                          <span className="font-medium">
                            Unproxied MCP Server
                          </span>
                          <span className="text-muted-foreground text-xs">
                            List a vendor server without proxying it (Speakeasy
                            staff only)
                          </span>
                        </div>
                      </DropdownMenuItem>
                    )}
                  </DropdownMenuContent>
                )}
              </DropdownMenu>
            )}
          </RequireScope>
          <PlatformMcpPromotion
            surface="sources_empty"
            projectSlug={projectSlug}
            className="mt-8 w-full max-w-2xl bg-card p-4 text-left"
          />
        </div>
      </Page.Section.Body>
    </Page.Section>
  );
}
