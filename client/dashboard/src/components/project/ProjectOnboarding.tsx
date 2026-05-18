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
          Secure and centrally manage MCPs, Skills, and Assistants your whole
          company can use, with fine-grained permissions, threat detection, and
          full observability.
        </p>
        <div className="flex flex-wrap gap-4">
          <routes.insights.tools.Link>
            <Button variant="primary">
              <Button.Text>Track AI usage</Button.Text>
              <Button.RightIcon>
                <Icon name="arrow-right" className="h-4 w-4" />
              </Button.RightIcon>
            </Button>
          </routes.insights.tools.Link>
          <routes.sources.Link>
            <Button variant="secondary">
              <Button.Text>Connect a Source</Button.Text>
              <Button.RightIcon>
                <Icon name="arrow-right" className="h-4 w-4" />
              </Button.RightIcon>
            </Button>
          </routes.sources.Link>
          <routes.policyCenter.Link>
            <Button variant="secondary">
              <Button.Text>Setup a security policy</Button.Text>
              <Button.RightIcon>
                <Icon name="arrow-right" className="h-4 w-4" />
              </Button.RightIcon>
            </Button>
          </routes.policyCenter.Link>
        </div>
      </div>
    </div>
  );
}
