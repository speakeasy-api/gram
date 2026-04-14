import { useTelemetry } from "@/contexts/Telemetry";
import { useRoutes } from "@/routes";
import { ListToolsetsQueryData } from "@gram/client/react-query";
import { Button } from "@speakeasy-api/moonshine";
import { BlocksIcon, Code, MessageCircleIcon, ServerIcon } from "lucide-react";
import { useMemo } from "react";

export type HomeOnboardingProps = {
  toolsets: ListToolsetsQueryData["toolsets"] | undefined;
};

export function HomeOnboarding({ toolsets }: HomeOnboardingProps) {
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const isFunctionsEnabled =
    telemetry.isFeatureEnabled("gram-functions") ?? false;

  // Get the first public MCP toolset slug to pass to elements page
  const firstPublicToolsetSlug = useMemo(() => {
    if (!toolsets) return undefined;
    const publicToolset = toolsets.find((t) => t.mcpIsPublic && t.mcpEnabled);
    return publicToolset?.slug;
  }, [toolsets]);

  return (
    <>
      <h2 className="mb-4 text-lg font-semibold">Quick actions</h2>
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <div className="bg-background relative flex flex-col gap-3 overflow-hidden rounded-lg border p-4 pb-5">
          <div className="bg-gradient-primary absolute inset-x-0 bottom-0 h-[3px]" />
          <div className="flex flex-row items-start gap-2">
            <MessageCircleIcon
              className="mt-0.5 h-[18px] w-[18px] shrink-0"
              strokeWidth={1.5}
            />
            <div className="flex flex-col gap-1">
              <h3 className="font-medium">Deploy chat</h3>
              <p className="text-muted-foreground text-sm">
                Embed an AI chat interface on your website with tool access
              </p>
            </div>
          </div>
          <div className="mt-auto flex justify-end">
            <routes.elements.Link
              className="no-underline"
              queryParams={
                firstPublicToolsetSlug
                  ? { toolset: firstPublicToolsetSlug }
                  : {}
              }
            >
              <Button size="sm">
                <Button.Text>Get started</Button.Text>
              </Button>
            </routes.elements.Link>
          </div>
        </div>
        <div className="bg-background relative flex flex-col gap-3 overflow-hidden rounded-lg border p-4 pb-5">
          <div className="bg-gradient-primary absolute inset-x-0 bottom-0 h-[3px]" />
          <div className="flex flex-row items-start gap-2">
            <BlocksIcon
              className="mt-0.5 h-[18px] w-[18px] shrink-0"
              strokeWidth={1.5}
            />
            <div className="flex flex-col gap-1">
              <h3 className="font-medium">Connect to popular tools</h3>
              <p className="text-muted-foreground text-sm">
                Browse and connect pre-built integrations from our catalog
              </p>
            </div>
          </div>
          <div className="mt-auto flex justify-end">
            <routes.catalog.Link className="no-underline">
              <Button size="sm">
                <Button.Text>Browse catalog</Button.Text>
              </Button>
            </routes.catalog.Link>
          </div>
        </div>
        <div className="bg-background relative flex flex-col gap-3 overflow-hidden rounded-lg border p-4 pb-5">
          <div className="bg-gradient-primary absolute inset-x-0 bottom-0 h-[3px]" />
          <div className="flex flex-row items-start gap-2">
            <ServerIcon
              className="mt-0.5 h-[18px] w-[18px] shrink-0"
              strokeWidth={1.5}
            />
            <div className="flex flex-col gap-1">
              <h3 className="font-medium">Connect to existing APIs</h3>
              <p className="text-muted-foreground text-sm">
                Create and deploy custom MCP servers from your APIs
              </p>
            </div>
          </div>
          <div className="mt-auto flex justify-end">
            <routes.sources.addOpenAPI.Link className="no-underline">
              <Button size="sm">
                <Button.Text>Upload OpenAPI</Button.Text>
              </Button>
            </routes.sources.addOpenAPI.Link>
          </div>
        </div>
        {isFunctionsEnabled && (
          <div className="bg-background relative flex flex-col gap-3 overflow-hidden rounded-lg border p-4 pb-5">
            <div className="bg-gradient-primary absolute inset-x-0 bottom-0 h-[3px]" />
            <div className="flex flex-row items-start gap-2">
              <Code
                className="mt-0.5 h-[18px] w-[18px] shrink-0"
                strokeWidth={1.5}
              />
              <div className="flex flex-col gap-1">
                <h3 className="font-medium">Build and host custom tools</h3>
                <p className="text-muted-foreground text-sm">
                  Write and deploy custom functions as MCP servers
                </p>
              </div>
            </div>
            <div className="mt-auto flex justify-end">
              <routes.sources.addFunction.Link className="no-underline">
                <Button size="sm">
                  <Button.Text>Deploy code</Button.Text>
                </Button>
              </routes.sources.addFunction.Link>
            </div>
          </div>
        )}
      </div>
    </>
  );
}
