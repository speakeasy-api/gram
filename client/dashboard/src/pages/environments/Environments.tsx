import { InputDialog } from "@/components/input-dialog";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/badge";
import { Card, Cards } from "@/components/ui/card";
import { UpdatedAt } from "@/components/updated-at";
import { useSession } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRoutes } from "@/routes";
import { Environment } from "@gram/client/models/components/environment.js";
import { useCreateEnvironmentMutation } from "@gram/client/react-query/index.js";
import { Plus } from "lucide-react";
import { useState } from "react";
import { Outlet } from "react-router";
import { Button } from "@speakeasy-api/moonshine";
import { handleAPIError } from "@/lib/errors";
import { useEnvironments } from "./useEnvironments";
export function EnvironmentsRoot() {
  return <Outlet />;
}

export default function Environments() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["build:read", "build:write"]} level="page">
          <EnvironmentsInner />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

function EnvironmentsInner() {
  const session = useSession();
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const environments = useEnvironments();

  const [createEnvironmentDialogOpen, setCreateEnvironmentDialogOpen] =
    useState(false);
  const [environmentName, setEnvironmentName] = useState("");

  const createEnvironmentMutation = useCreateEnvironmentMutation({
    onSuccess: async (data) => {
      telemetry.capture("environment_event", {
        action: "environment_created",
        environment_slug: data.slug,
      });
      routes.environments.environment.goTo(data.slug);
    },
    onError: (error) => {
      handleAPIError(error, "Failed to create environment");
      telemetry.capture("environment_event", {
        action: "environment_creation_failed",
        error: error.message,
      });
    },
  });

  const createEnvironment = () => {
    createEnvironmentMutation.mutate({
      request: {
        createEnvironmentForm: {
          name: environmentName,
          description: "New Environment Description",
          entries: [],
          organizationId: session.activeOrganizationId,
        },
      },
    });
  };

  return (
    <>
      <Page.Section>
        <Page.Section.Title>Environments</Page.Section.Title>
        <Page.Section.Description>
          Use environments to manage API keys, allowing Gram to handle
          authentication for you
        </Page.Section.Description>
        <Page.Section.CTA>
          <RequireScope scope="build:write" level="component">
            <Button onClick={() => setCreateEnvironmentDialogOpen(true)}>
              <Button.LeftIcon>
                <Plus className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>New Environment</Button.Text>
            </Button>
          </RequireScope>
        </Page.Section.CTA>
        <Page.Section.Body>
          <Cards>
            {environments.map((environment) => (
              <EnvironmentCard key={environment.id} environment={environment} />
            ))}
          </Cards>
        </Page.Section.Body>
      </Page.Section>
      <InputDialog
        open={createEnvironmentDialogOpen}
        onOpenChange={setCreateEnvironmentDialogOpen}
        title="Create an Environment"
        description="Give your environment a name."
        inputs={{
          label: "Environment name",
          placeholder: "Environment name",
          value: environmentName,
          onChange: (value) => setEnvironmentName(value),
          onSubmit: createEnvironment,
          validate: (value) => value.length > 0,
        }}
      />
    </>
  );
}

function EnvironmentCard({ environment }: { environment: Environment }) {
  const routes = useRoutes();

  return (
    <routes.environments.environment.Link
      params={[environment.slug]}
      className="hover:no-underline"
    >
      <Card>
        <Card.Header>
          <Card.Title>{environment.name}</Card.Title>
        </Card.Header>
        <Card.Content>
          <Card.Description>
            {environment.description || "No description provided"}
          </Card.Description>
        </Card.Content>
        <Card.Footer>
          <Badge variant="outline">
            {environment.entries.length || "No"} Entries
          </Badge>
          <UpdatedAt date={new Date(environment.updatedAt)} />
        </Card.Footer>
      </Card>
    </routes.environments.environment.Link>
  );
}
