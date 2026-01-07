import { Page } from "@/components/page-layout";
import { Type } from "@/components/ui/type";
import { useRoutes } from "@/routes";
import { Stack } from "@speakeasy-api/moonshine";
import { ArrowRight, Check, Code, Database, Rocket } from "lucide-react";
import { Link } from "react-router";

const ELEMENTS_ONBOARDING_KEY = "elements-onboarding-completed";

export default function ChatElements() {
  const routes = useRoutes();
  const isStep1Completed =
    localStorage.getItem(ELEMENTS_ONBOARDING_KEY) === "true";

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title>Chat Elements</Page.Section.Title>
          <Page.Section.Description>
            Embeddable chat components for your applications
          </Page.Section.Description>
          <Page.Section.Body>
            <div className="grid grid-cols-1 lg:grid-cols-3 gap-6 mt-4">
              <StepCard
                step={1}
                icon={Code}
                title="Add Elements to your application"
                description="Install the @gram-ai/elements package and add the chat component to your React app"
                linkTo={routes.deployChat.href()}
                linkText={isStep1Completed ? "View setup" : "Get started"}
                completed={isStep1Completed}
              />
              <StepCard
                step={2}
                icon={Database}
                title="Connect to your data"
                description="Create tools from your APIs or connect to existing MCP servers to power your chat"
                linkTo={
                  routes.onboarding.href() + "?start-step=first-party-choice"
                }
                linkText="Connect data"
              />
              <StepCard
                step={3}
                icon={Rocket}
                title="Productionize your chat"
                description="Set up Chat Sessions for secure production deployments and customize your chat experience"
                // TODO: Update these docs to actually talk about chat sessions
                linkTo="https://www.getgram.ai/docs/gram-elements/quickstart#step-2-setup-your-backend"
                linkText="View docs"
                external
              />
            </div>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}

const StepCard = ({
  step,
  icon: Icon,
  title,
  description,
  linkTo,
  linkText,
  external,
  completed,
}: {
  step: number;
  icon: React.ComponentType<{ className?: string; strokeWidth?: number }>;
  title: string;
  description: string;
  linkTo: string;
  linkText: string;
  external?: boolean;
  completed?: boolean;
}) => {
  const content = (
    <div className="h-full p-6 bg-card border rounded-lg hover:bg-accent transition-colors text-left group flex flex-col">
      <div className="flex items-center gap-3 mb-4">
        {completed ? (
          <div className="flex items-center justify-center w-8 h-8 rounded-full bg-success text-success-foreground">
            <Check className="w-5 h-5" strokeWidth={2.5} />
          </div>
        ) : (
          <div className="flex items-center justify-center w-8 h-8 rounded-full bg-primary/10 text-primary text-sm font-medium">
            {step}
          </div>
        )}
        <Icon className="w-6 h-6 text-muted-foreground" strokeWidth={1.5} />
      </div>
      <Stack gap={2} className="flex-1">
        <Type className="text-heading-sm">{title}</Type>
        <Type small className="text-muted-foreground">
          {description}
        </Type>
      </Stack>
      <div className="flex items-center gap-1 mt-4 text-primary text-sm font-medium group-hover:gap-2 transition-all">
        {linkText}
        <ArrowRight className="w-4 h-4" />
      </div>
    </div>
  );

  if (external) {
    return (
      <a href={linkTo} target="_blank" rel="noopener noreferrer">
        {content}
      </a>
    );
  }

  return <Link to={linkTo}>{content}</Link>;
};
