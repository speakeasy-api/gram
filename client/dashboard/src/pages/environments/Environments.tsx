import { AddButton } from "@/components/add-button";
import { InputDialog } from "@/components/input-dialog";
import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { HumanizeDateTime } from "@/lib/dates";
import { useRoutes } from "@/routes";
import { Environment } from "@gram/client/models/components/environment.js";
import {
  useCreateEnvironmentMutation,
  useListEnvironmentsSuspense,
} from "@gram/client/react-query/index.js";
import { Stack } from "@speakeasy-api/moonshine";
import { useState } from "react";
import { Outlet } from "react-router";
import { CreateThingCard } from "../toolsets/Toolsets";

export function EnvironmentsRoot() {
  return <Outlet />;
}

export function useEnvironments() {
  const { data: environments, refetch: refetchEnvironments } =
    useListEnvironmentsSuspense(undefined, undefined, {
      refetchOnWindowFocus: false,
    });
  return Object.assign(environments.environments, {
    refetch: refetchEnvironments,
  });
}

export default function Environments() {
  const project = useProject();
  const environments = useEnvironments();
  const routes = useRoutes();

  const [createEnvironmentDialogOpen, setCreateEnvironmentDialogOpen] =
    useState(false);
  const [environmentName, setEnvironmentName] = useState("");
  const createEnvironmentMutation = useCreateEnvironmentMutation({
    onSuccess: async (data) => {
      await environments.refetch();
      routes.environments.environment.goTo(data.slug);
    },
    onError: (error) => {
      console.error("Failed to create environment:", error);
    },
  });

  const createEnvironment = () => {
    createEnvironmentMutation.mutate({
      request: {
        createEnvironmentForm: {
          name: environmentName,
          description: "New Environment Description",
          entries: [],
          organizationId: project.organizationId,
        },
      },
    });
  };

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
        <Page.Header.Actions>
          <AddButton
            onClick={() => setCreateEnvironmentDialogOpen(true)}
            tooltip="New Environment"
          />
        </Page.Header.Actions>
      </Page.Header>
      <Page.Body>
        {environments.map((environment) => (
          <EnvironmentCard key={environment.id} environment={environment} />
        ))}
        <CreateThingCard onClick={() => setCreateEnvironmentDialogOpen(true)}>
          + New Environment
        </CreateThingCard>
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
      </Page.Body>
    </Page>
  );
}

function EnvironmentCard({ environment }: { environment: Environment }) {
  const routes = useRoutes();

  return (
    <Card>
      <Card.Header>
        <Stack direction="horizontal" gap={2} justify={"space-between"}>
          <routes.environments.environment.Link params={[environment.slug]}>
            <Card.Title className="hover:underline">
              {environment.name}
            </Card.Title>
          </routes.environments.environment.Link>
          <Badge>{environment.entries.length || "No"} Entries</Badge>
        </Stack>
        <Stack direction="horizontal" gap={3} justify={"space-between"}>
          {/* TODO: add description */}
          <Type variant="body" muted className="text-sm italic">
            {"Updated "}
            <HumanizeDateTime date={new Date(environment.updatedAt)} />
          </Type>
        </Stack>
      </Card.Header>
      <Card.Content>
        <routes.environments.environment.Link params={[environment.slug]}>
          <Button variant="outline">Edit</Button>
        </routes.environments.environment.Link>
      </Card.Content>
    </Card>
  );
}
