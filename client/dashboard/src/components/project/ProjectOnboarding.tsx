import { useRoutes } from "@/routes";
import { Button, Icon } from "@speakeasy-api/moonshine";

export function ProjectOnboardingBanner() {
  const routes = useRoutes();

  return (
    <div className="bg-background relative overflow-hidden rounded-lg border p-8">
      <div className="bg-gradient-primary absolute inset-x-0 bottom-0 h-[3px]" />
      <h2 className="mb-4 text-3xl font-light">Welcome</h2>
      <div className="flex max-w-lg flex-col gap-8">
        <p className="text-muted-foreground text-base">
          Build and deploy MCP servers in minutes. Connect your APIs, browse
          popular integrations, or deploy a chat interface — all from one place.
        </p>
        <div className="flex flex-wrap gap-4">
          <routes.sources.Link>
            <Button variant="primary">
              <Button.Text>Connect an API</Button.Text>
              <Button.RightIcon>
                <Icon name="arrow-right" className="h-4 w-4" />
              </Button.RightIcon>
            </Button>
          </routes.sources.Link>
          <routes.catalog.Link>
            <Button variant="secondary">
              <Button.Text>Browse catalog</Button.Text>
              <Button.RightIcon>
                <Icon name="arrow-right" className="h-4 w-4" />
              </Button.RightIcon>
            </Button>
          </routes.catalog.Link>
        </div>
      </div>
    </div>
  );
}
