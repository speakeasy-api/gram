import {
  FooterSaveButton,
  SettingsSection,
} from "@/components/detail/settings-section";
import { RequireScope } from "@/components/require-scope";
import { Field, FieldError, FieldLabel } from "@/components/ui/Field";
import { Checkbox } from "@/components/ui/Checkbox";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { useProject as useProjectDetails } from "@gram/client/react-query/project";
import { Input } from "@/components/ui/Input";
import { useOrganization, useProject, useSession } from "@/contexts/Auth";
import type { SessionInfoResponse } from "@gram/client/models/operations/sessioninfo.js";
import { invalidateAllListProjects } from "@gram/client/react-query/listProjects";
import { invalidateAllProject } from "@gram/client/react-query/project";
import { useUpdateProjectMutation } from "@gram/client/react-query/updateProject";
import { useEffect, useState } from "react";
import { useForm, useStore } from "@tanstack/react-form";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { z } from "zod";

const PROJECT_NAME_MAX_LENGTH = 40;

const projectNameSchema = z
  .string()
  .trim()
  .min(1, "Enter a project name.")
  .refine(
    (name) => Array.from(name).length <= PROJECT_NAME_MAX_LENGTH,
    `Project name must be ${PROJECT_NAME_MAX_LENGTH} characters or fewer.`,
  );

function firstError(errors: unknown[]): string | undefined {
  const error = errors.find(Boolean);
  if (typeof error === "string") return error;
  if (
    error &&
    typeof error === "object" &&
    "message" in error &&
    typeof error.message === "string"
  ) {
    return error.message;
  }
  return undefined;
}

export function ProjectNameSection(): React.ReactNode {
  const project = useProject();

  const details = useProjectDetails({ slug: project.slug });
  if (details.isPending) return <Skeleton className="h-64 w-full" />;
  if (details.isError)
    return (
      <FieldError>
        Could not load project settings.{" "}
        <Button variant="secondary" onClick={() => void details.refetch()}>
          Retry
        </Button>
      </FieldError>
    );
  return (
    <ProjectNameForm
      key={project.id}
      project={{ ...project, name: details.data.project.name }}
      requireMcpGateway={details.data.project.requireMcpGateway ?? false}
    />
  );
}

function ProjectNameForm({
  project,
  requireMcpGateway,
}: {
  project: ReturnType<typeof useProject>;
  requireMcpGateway: boolean;
}): React.ReactNode {
  const organization = useOrganization();
  const session = useSession();
  const queryClient = useQueryClient();
  const [defaultValues, setDefaultValues] = useState({
    name: project.name,
    requireMcpGateway,
  });

  const update = useUpdateProjectMutation({
    onSuccess: async ({ project: updatedProject }) => {
      const savedValues = {
        name: updatedProject.name,
        requireMcpGateway: updatedProject.requireMcpGateway ?? false,
      };
      setDefaultValues(savedValues);
      form.reset(savedValues);

      queryClient.setQueriesData<SessionInfoResponse>(
        { queryKey: ["@gram/client", "auth", "info"] },
        (session) =>
          session
            ? {
                ...session,
                result: {
                  ...session.result,
                  organizations: session.result.organizations.map((org) => ({
                    ...org,
                    projects: org.projects.map((cachedProject) =>
                      cachedProject.id === project.id
                        ? { ...cachedProject, name: updatedProject.name }
                        : cachedProject,
                    ),
                  })),
                },
              }
            : session,
      );

      await Promise.allSettled([
        organization.refetch(),
        invalidateAllListProjects(queryClient),
        invalidateAllProject(queryClient),
      ]);

      toast.success("Project settings updated");
    },
  });

  const form = useForm({
    defaultValues,
    onSubmit: ({ value }) => {
      update.mutate({
        request: {
          updateProjectForm: {
            name: projectNameSchema.parse(value.name),
            ...(value.requireMcpGateway !== defaultValues.requireMcpGateway
              ? { requireMcpGateway: value.requireMcpGateway }
              : {}),
          },
        },
        security: {
          projectSlugHeaderGramProject: project.slug,
          sessionHeaderGramSession: session.session,
        },
      });
    },
  });

  const isDirty = useStore(form.store, (state) => state.isDirty);
  useEffect(() => {
    if (!isDirty) return;
    const warn = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [isDirty]);

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Project details</SettingsSection.Title>
        <SettingsSection.Description>
          View and update your project details.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <form
        onSubmit={(event) => {
          event.preventDefault();
          void form.handleSubmit();
        }}
      >
        <SettingsSection.Panel>
          <SettingsSection.Body>
            <div className="grid gap-4 md:grid-cols-2">
              <form.Field
                name="name"
                validators={{
                  onChange: projectNameSchema,
                  onSubmit: projectNameSchema,
                }}
              >
                {(field) => {
                  const error = firstError(field.state.meta.errors);
                  return (
                    <Field data-invalid={error ? true : undefined}>
                      <FieldLabel htmlFor="project-display-name">
                        Display Name
                      </FieldLabel>
                      <Input
                        id="project-display-name"
                        name={field.name}
                        value={field.state.value}
                        onChange={(value) => {
                          update.reset();
                          field.handleChange(value);
                        }}
                        onBlur={field.handleBlur}
                        disabled={update.isPending}
                        aria-invalid={Boolean(error)}
                        aria-describedby={
                          error ? "project-display-name-error" : undefined
                        }
                      />
                      {error && (
                        <FieldError id="project-display-name-error">
                          {error}
                        </FieldError>
                      )}
                    </Field>
                  );
                }}
              </form.Field>
              <Field>
                <FieldLabel htmlFor="project-slug">Slug</FieldLabel>
                <Input id="project-slug" value={project.slug} disabled />
              </Field>
            </div>
            <form.Field name="requireMcpGateway">
              {(field) => (
                <Field>
                  <div className="flex items-center gap-2">
                    <Checkbox
                      id="require-mcp-gateway"
                      checked={field.state.value}
                      onCheckedChange={(checked) => {
                        update.reset();
                        field.handleChange(checked === true);
                      }}
                      disabled={update.isPending}
                      aria-describedby="require-mcp-gateway-description"
                    />
                    <FieldLabel htmlFor="require-mcp-gateway">
                      Require an MCP gateway
                    </FieldLabel>
                  </div>
                  <p
                    id="require-mcp-gateway-description"
                    className="text-sm text-muted-foreground"
                  >
                    Block direct connections to this project's MCP servers,
                    including existing connections. Set up a gateway and give
                    users its URL before saving.
                  </p>
                </Field>
              )}
            </form.Field>
            {update.isError && (
              <FieldError className="max-w-md">
                Could not save project settings. Try again.
              </FieldError>
            )}
          </SettingsSection.Body>
          <form.Subscribe
            selector={(state) => [state.isDirty, state.canSubmit] as const}
          >
            {([isDirty, canSubmit]) => (
              <SettingsSection.Footer>
                <SettingsSection.FooterHint>
                  <span role="status" aria-live="polite">
                    {isDirty ? "Unsaved changes" : ""}
                  </span>
                </SettingsSection.FooterHint>
                <SettingsSection.FooterActions>
                  <RequireScope
                    scope="project:write"
                    resourceId={project.id}
                    level="component"
                  >
                    <FooterSaveButton
                      type="submit"
                      pending={update.isPending}
                      disabled={!isDirty || !canSubmit || update.isPending}
                    />
                  </RequireScope>
                </SettingsSection.FooterActions>
              </SettingsSection.Footer>
            )}
          </form.Subscribe>
        </SettingsSection.Panel>
      </form>
    </SettingsSection>
  );
}
