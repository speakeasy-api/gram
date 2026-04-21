import { useRoutes } from "@/routes";
import { Card, Button, Icon } from "@speakeasy-api/moonshine";

export function ProjectOnboardingBanner() {
  const routes = useRoutes();

  return (
    <Card className="bg-background p-8">
      <Card.Header>
        <h2 className="text-3xl font-light">Welcome</h2>
      </Card.Header>
      <Card.Content className="flex max-w-lg flex-col gap-8">
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
      </Card.Content>
    </Card>
  );
}
