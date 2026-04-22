import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Type } from "@/components/ui/type";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRoutes } from "@/routes";
import {
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@speakeasy-api/moonshine";
import {
  ChevronDown,
  Code,
  Database,
  FileCode,
  Plus,
  Server,
} from "lucide-react";

export function SourcesEmptyState() {
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const isFunctionsEnabled =
    telemetry.isFeatureEnabled("gram-functions") ?? false;

  return (
    <Page.Section>
      <Page.Section.Title>Sources</Page.Section.Title>
      <Page.Section.Description className="max-w-2xl">
        {isFunctionsEnabled
          ? "OpenAPI documents, Gram Functions, and third-party MCP servers providing tools for your project"
          : "OpenAPI documents and third-party MCP servers providing tools for your project"}
      </Page.Section.Description>
      <Page.Section.Body>
        <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16">
          <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
            <Database className="text-muted-foreground h-6 w-6" />
          </div>
          <Type variant="subheading" className="mb-1">
            No sources yet
          </Type>
          <Type small muted className="mb-4 max-w-md text-center">
            Add an OpenAPI spec, custom function, or third-party server to
            generate tools for your MCP server.
          </Type>
          <RequireScope scope="build:write" level="component">
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
                  <DropdownMenuContent align="center" className="w-[320px] p-1">
                    <DropdownMenuItem
                      onSelect={() => routes.sources.addOpenAPI.goTo()}
                      className="flex cursor-pointer items-start gap-3 rounded-md p-2"
                    >
                      <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-blue-500/10 dark:bg-blue-500/20">
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
                        className="flex cursor-pointer items-start gap-3 rounded-md p-2"
                      >
                        <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-emerald-500/10 dark:bg-emerald-500/20">
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
                      className="flex cursor-pointer items-start gap-3 rounded-md p-2"
                    >
                      <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-violet-500/10 dark:bg-violet-500/20">
                        <Server className="h-5 w-5 text-violet-600 dark:text-violet-400" />
                      </div>
                      <div className="flex flex-col gap-0.5">
                        <span className="font-medium">Third party server</span>
                        <span className="text-muted-foreground text-xs">
                          Add pre-built servers from the catalog
                        </span>
                      </div>
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                )}
              </DropdownMenu>
            )}
          </RequireScope>
        </div>
      </Page.Section.Body>
    </Page.Section>
  );
}
