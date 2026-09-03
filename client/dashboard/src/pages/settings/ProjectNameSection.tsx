import {
  FooterSaveButton,
  SettingsSection,
} from "@/components/detail/settings-section";
import { RequireScope } from "@/components/require-scope";
import { Field, FieldError, FieldLabel } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { useOrganization, useProject, useSession } from "@/contexts/Auth";
import type { SessionInfoResponse } from "@gram/client/models/operations/sessioninfo.js";
import { invalidateAllListProjects } from "@gram/client/react-query/listProjects";
import { useUpdateProjectMutation } from "@gram/client/react-query/updateProject";
import { useForm } from "@tanstack/react-form";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { z } from "zod";

const PROJECT_NAME_MAX_LENGTH = 40;

const projectNameSchema = z
  .string()
  .trim()
  .min(1, "Enter a project name.")
  .max(
    PROJECT_NAME_MAX_LENGTH,
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

  return <ProjectNameForm key={project.id} project={project} />;
}

function ProjectNameForm({
  project,
}: {
  project: ReturnType<typeof useProject>;
}): React.ReactNode {
  const organization = useOrganization();
  const session = useSession();
  const queryClient = useQueryClient();

  const update = useUpdateProjectMutation({
    onSuccess: async ({ project: updatedProject }) => {
      form.reset({ name: updatedProject.name });

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
      ]);

      toast.success("Project name updated");
    },
  });

  const form = useForm({
    defaultValues: { name: project.name },
    onSubmit: ({ value }) => {
      update.mutate({
        request: {
          updateProjectForm: { name: projectNameSchema.parse(value.name) },
        },
        security: {
          projectSlugHeaderGramProject: project.slug,
          sessionHeaderGramSession: session.session,
        },
      });
    },
  });

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
                        maxLength={PROJECT_NAME_MAX_LENGTH}
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
            {update.isError && (
              <FieldError className="max-w-md">
                {update.error.message}
              </FieldError>
            )}
          </SettingsSection.Body>
          <form.Subscribe
            selector={(state) => [state.isDirty, state.canSubmit] as const}
          >
            {([isDirty, canSubmit]) => (
              <SettingsSection.Footer>
                <SettingsSection.FooterHint>
                  {isDirty ? "Unsaved changes" : ""}
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
