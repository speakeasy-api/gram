import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Type } from "@/components/ui/type";
import { HumanizeDateTime } from "@/lib/dates";
import {
  useCreateEnvironmentMutation,
  useListEnvironmentsSuspense,
} from "@gram/sdk/react-query";
import { Stack } from "@speakeasy-api/moonshine";
import { Link, Outlet, useNavigate } from "react-router-dom";
import { useProject } from "@/contexts/Auth";
import { useState } from "react";
import { PlusIcon } from "lucide-react";
import { Environment } from "@gram/sdk/models/components/environment.js";
import { CreateThingCard } from "../toolsets/Toolsets";
import { InputDialog } from "@/components/input-dialog";

export function EnvironmentsRoot() {
  return <Outlet />;
}

export function useEnvironments() {
  const project = useProject();
  const { data: environments, refetch: refetchEnvironments } =
    useListEnvironmentsSuspense({
      gramProject: project.projectSlug,
    });
  return Object.assign(environments.environments, {
    refetch: refetchEnvironments,
  });
}

export default function Environments() {
  const project = useProject();
  const navigate = useNavigate();
  const environments = useEnvironments();

  const [createEnvironmentDialogOpen, setCreateEnvironmentDialogOpen] =
    useState(false);
  const [environmentName, setEnvironmentName] = useState("");
  const createEnvironmentMutation = useCreateEnvironmentMutation({
    onSuccess: (data) => {
      environments.refetch();
      navigate(`/environments/${data.slug}`);
    },
    onError: (error) => {
      console.error("Failed to create environment:", error);
    },
  });

  const createEnvironment = () => {
    createEnvironmentMutation.mutate({
      request: {
        gramProject: project.projectSlug,
        createEnvironmentForm: {
          name: environmentName,
          description: "New Environment Description",
          entries: [],
          organizationId: project.organizationId,
        },
      },
    });
  };

  const addButton = (
    <Button
      variant="ghost"
      className="text-muted-foreground hover:text-foreground"
      onClick={() => {
        setCreateEnvironmentDialogOpen(true);
      }}
      tooltip="New Environment"
    >
      <PlusIcon className="w-4 h-4" />
    </Button>
  );

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
        <Page.Header.Actions>{addButton}</Page.Header.Actions>
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
  return (
    <Card>
      <Card.Header>
        <Stack direction="horizontal" gap={2} justify={"space-between"}>
          <Link
            to={`/environments/${environment.slug}`}
            className="hover:underline"
          >
            <Card.Title>{environment.name}</Card.Title>
          </Link>
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
        <Link to={`/environments/${environment.slug}`}>
          <Button variant="outline">Edit</Button>
        </Link>
      </Card.Content>
    </Card>
  );
}
