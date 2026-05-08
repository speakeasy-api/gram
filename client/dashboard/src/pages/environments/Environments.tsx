import { InputDialog } from "@/components/input-dialog";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { DotCard } from "@/components/ui/dot-card";
import { MoreActions } from "@/components/ui/more-actions";
import { useSession } from "@/contexts/Auth";
import { useTelemetry } from "@/contexts/Telemetry";
import { useRoutes } from "@/routes";
import { Environment } from "@gram/client/models/components/environment.js";
import { useCreateEnvironmentMutation } from "@gram/client/react-query/index.js";
import { ArrowRight, Blocks, Plus } from "lucide-react";
import { useState } from "react";
import { Outlet } from "react-router";
import { Badge, Button } from "@speakeasy-api/moonshine";
import { Type } from "@/components/ui/type";
import { handleAPIError } from "@/lib/errors";
import { CloneEnvironmentDialog } from "./CloneEnvironmentDialog";
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
        <RequireScope scope={["project:read", "project:write"]} level="page">
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
  const [cloneSource, setCloneSource] = useState<Environment | null>(null);

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
          Create re-usable environment configurations and share amongst multiple
          MCP servers
        </Page.Section.Description>
        <Page.Section.CTA>
          {environments.length > 0 && (
            <RequireScope scope="project:write" level="component">
              <Button onClick={() => setCreateEnvironmentDialogOpen(true)}>
                <Button.LeftIcon>
                  <Plus className="h-4 w-4" />
                </Button.LeftIcon>
                <Button.Text>New Environment</Button.Text>
              </Button>
            </RequireScope>
          )}
        </Page.Section.CTA>
        <Page.Section.Body>
          {environments.length === 0 ? (
            <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16">
              <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
                <Blocks className="text-muted-foreground h-6 w-6" />
              </div>
              <Type variant="subheading" className="mb-1">
                No environments yet
              </Type>
              <Type small muted className="mb-4 max-w-md text-center">
                Environments let you store configuration and secrets that can be
                shared across multiple MCP servers.
              </Type>
              <RequireScope scope="project:write" level="component">
                <Button onClick={() => setCreateEnvironmentDialogOpen(true)}>
                  <Button.LeftIcon>
                    <Plus className="h-4 w-4" />
                  </Button.LeftIcon>
                  <Button.Text>New Environment</Button.Text>
                </Button>
              </RequireScope>
            </div>
          ) : (
            <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
              {environments.map((environment) => (
                <EnvironmentCard
                  key={environment.id}
                  environment={environment}
                  onClone={setCloneSource}
                />
              ))}
            </div>
          )}
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
      <CloneEnvironmentDialog
        key={cloneSource?.id ?? ""}
        source={cloneSource}
        open={cloneSource !== null}
        onOpenChange={(open) => {
          if (!open) setCloneSource(null);
        }}
      />
    </>
  );
}

function EnvironmentCard({
  environment,
  onClone,
}: {
  environment: Environment;
  onClone: (environment: Environment) => void;
}) {
  const routes = useRoutes();

  return (
    <routes.environments.environment.Link
      params={[environment.slug]}
      className="hover:no-underline"
    >
      <DotCard icon={<Blocks className="text-muted-foreground h-8 w-8" />}>
        <div className="mb-2 flex items-start justify-between gap-2">
          <Type
            variant="subheading"
            as="div"
            className="text-md group-hover:text-primary flex-1 truncate transition-colors"
            title={environment.name}
          >
            {environment.name}
          </Type>
          <RequireScope scope="environment:write" level="component">
            <div onClick={(e) => e.stopPropagation()}>
              <MoreActions
                actions={[
                  {
                    label: "Clone",
                    onClick: () => onClone(environment),
                    icon: "copy",
                  },
                ]}
              />
            </div>
          </RequireScope>
        </div>
        <Type small muted className="truncate">
          {environment.description || "No description provided"}
        </Type>
        <div className="mt-auto flex items-center justify-between gap-2 pt-2">
          <Badge variant="neutral">
            {environment.entries.length}{" "}
            {environment.entries.length === 1 ? "Entry" : "Entries"}
          </Badge>
          <div className="text-muted-foreground group-hover:text-primary flex items-center gap-1 text-sm transition-colors">
            <span>Open</span>
            <ArrowRight className="h-3.5 w-3.5" />
          </div>
        </div>
      </DotCard>
    </routes.environments.environment.Link>
  );
}
