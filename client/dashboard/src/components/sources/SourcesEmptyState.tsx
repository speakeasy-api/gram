import { Page } from "@/components/page-layout";
import { Type } from "@/components/ui/type";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRoutes } from "@/routes";
import {
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Stack,
} from "@speakeasy-api/moonshine";
import { ChevronDown, Code, Database, FileCode, Plus, Server } from "lucide-react";

export function SourcesEmptyState() {
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const isFunctionsEnabled =
    telemetry.isFeatureEnabled("gram-functions") ?? false;

  return (
    <Page.Section>
      <Page.Section.Title>Sources</Page.Section.Title>
      <Page.Section.Description>
        {isFunctionsEnabled
          ? "OpenAPI documents and Gram Functions providing tools for your project"
          : "OpenAPI documents providing tools for your project"}
      </Page.Section.Description>
      <Page.Section.Body>
        <div className="flex flex-col items-center justify-center py-16 px-8 rounded-xl border border-dashed bg-muted/20">
          <div className="w-12 h-12 rounded-full bg-muted/50 flex items-center justify-center mb-4">
            <Database className="w-6 h-6 text-muted-foreground" />
          </div>
          <Type variant="subheading" className="mb-1">No sources yet</Type>
          <Type small muted className="text-center mb-4 max-w-md">
            Add an OpenAPI spec, custom function, or third-party server to generate tools for your MCP server.
          </Type>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button>
                <Button.LeftIcon>
                  <Plus className="w-4 h-4" />
                </Button.LeftIcon>
                <Button.Text>Add Source</Button.Text>
                <ChevronDown className="w-4 h-4 ml-1" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="center" className="w-[320px] p-1">
              <DropdownMenuItem
                onSelect={() => routes.sources.addOpenAPI.goTo()}
                className="cursor-pointer flex items-start gap-3 p-2 rounded-md"
              >
                <div className="w-10 h-10 rounded-lg bg-blue-500/10 dark:bg-blue-500/20 flex items-center justify-center shrink-0">
                  <FileCode className="w-5 h-5 text-blue-600 dark:text-blue-400" />
                </div>
                <div className="flex flex-col gap-0.5">
                  <span className="font-medium">From your API</span>
                  <span className="text-xs text-muted-foreground">Upload an OpenAPI spec to generate tools</span>
                </div>
              </DropdownMenuItem>
              {isFunctionsEnabled && (
                <DropdownMenuItem
                  onSelect={() => routes.sources.addFunction.goTo()}
                  className="cursor-pointer flex items-start gap-3 p-2 rounded-md"
                >
                  <div className="w-10 h-10 rounded-lg bg-emerald-500/10 dark:bg-emerald-500/20 flex items-center justify-center shrink-0">
                    <Code className="w-5 h-5 text-emerald-600 dark:text-emerald-400" />
                  </div>
                  <div className="flex flex-col gap-0.5">
                    <span className="font-medium">Write custom code</span>
                    <span className="text-xs text-muted-foreground">Create tools with TypeScript functions</span>
                  </div>
                </DropdownMenuItem>
              )}
              <DropdownMenuItem
                onSelect={() => routes.sources.addFromCatalog.goTo()}
                className="cursor-pointer flex items-start gap-3 p-2 rounded-md"
              >
                <div className="w-10 h-10 rounded-lg bg-violet-500/10 dark:bg-violet-500/20 flex items-center justify-center shrink-0">
                  <Server className="w-5 h-5 text-violet-600 dark:text-violet-400" />
                </div>
                <div className="flex flex-col gap-0.5">
                  <span className="font-medium">Third party server</span>
                  <span className="text-xs text-muted-foreground">Add pre-built servers from the catalog</span>
                </div>
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </Page.Section.Body>
    </Page.Section>
  );
}
