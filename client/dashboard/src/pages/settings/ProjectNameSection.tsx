import {
  FooterSaveButton,
  SettingsSection,
} from "@/components/detail/settings-section";
import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { useOrganization, useProject } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import type { SessionInfoResponse } from "@gram/client/models/operations/sessioninfo.js";
import { invalidateAllListProjects } from "@gram/client/react-query/listProjects";
import { useUpdateProjectMutation } from "@gram/client/react-query/updateProject";
import { useForm } from "@tanstack/react-form";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";
import { z } from "zod";

const PROJECT_NAME_MAX_LENGTH = 40;
const PROJECT_SLUG_MAX_LENGTH = 40;

const projectNameSchema = z
  .string()
  .trim()
  .min(1, "Enter a project name.")
  .max(
    PROJECT_NAME_MAX_LENGTH,
    `Project name must be ${PROJECT_NAME_MAX_LENGTH} characters or fewer.`,
  );

const projectSlugSchema = z
  .string()
  .trim()
  .min(1, "Enter a project slug.")
  .max(
    PROJECT_SLUG_MAX_LENGTH,
    `Project slug must be ${PROJECT_SLUG_MAX_LENGTH} characters or fewer.`,
  )
  .regex(
    /^[a-z0-9_-]+$/,
    "Use only lowercase letters, numbers, dashes, and underscores.",
  );

const projectDetailsSchema = z.object({
  name: projectNameSchema,
  slug: projectSlugSchema,
});

type ProjectDetails = z.infer<typeof projectDetailsSchema>;

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
  const { orgSlug } = useSlugs();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [slugWarningOpen, setSlugWarningOpen] = useState(false);
  const [pendingSlugChange, setPendingSlugChange] =
    useState<ProjectDetails | null>(null);
  const isDefaultProject = project.slug === "default";

  const update = useUpdateProjectMutation({
    onSuccess: async ({ project: updatedProject }) => {
      const slugChanged = updatedProject.slug !== project.slug;

      form.reset({
        name: updatedProject.name,
        slug: updatedProject.slug,
      });
      setSlugWarningOpen(false);
      setPendingSlugChange(null);

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
                        ? {
                            ...cachedProject,
                            name: updatedProject.name,
                            slug: updatedProject.slug,
                          }
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

      if (slugChanged && orgSlug) {
        void navigate(`/${orgSlug}/projects/${updatedProject.slug}/settings`, {
          replace: true,
        });
      }
      toast.success("Project details updated");
    },
  });

  const save = (values: ProjectDetails) =>
    update.mutate({
      request: {
        updateProjectForm: values,
      },
    });

  const form = useForm({
    defaultValues: {
      name: project.name,
      slug: project.slug,
    },
    onSubmit: ({ value }) => {
      const values = projectDetailsSchema.parse(value);
      if (values.slug !== project.slug) {
        setPendingSlugChange(values);
        setSlugWarningOpen(true);
        return;
      }
      save(values);
    },
  });

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Project details</SettingsSection.Title>
        <SettingsSection.Description>
          Change the project name and URL slug.
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
                  <Field
                    data-invalid={error || update.isError ? true : undefined}
                    className="max-w-md"
                  >
                    <FieldLabel htmlFor="project-display-name">
                      Project name
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
                      aria-invalid={Boolean(error || update.isError)}
                      aria-describedby={
                        error || update.isError
                          ? "project-display-name-error"
                          : undefined
                      }
                    />
                    {(error || update.isError) && (
                      <FieldError id="project-display-name-error">
                        {error ?? update.error?.message}
                      </FieldError>
                    )}
                  </Field>
                );
              }}
            </form.Field>
            <form.Field
              name="slug"
              validators={{
                onChange: projectSlugSchema,
                onSubmit: projectSlugSchema,
              }}
            >
              {(field) => {
                const error = firstError(field.state.meta.errors);
                return (
                  <Field
                    data-invalid={error || update.isError ? true : undefined}
                    className="max-w-md"
                  >
                    <FieldLabel htmlFor="project-slug">Project slug</FieldLabel>
                    <Input
                      id="project-slug"
                      name={field.name}
                      value={field.state.value}
                      onChange={(value) => {
                        update.reset();
                        field.handleChange(value);
                      }}
                      onBlur={field.handleBlur}
                      disabled={update.isPending || isDefaultProject}
                      maxLength={PROJECT_SLUG_MAX_LENGTH}
                      aria-invalid={Boolean(error || update.isError)}
                      aria-describedby={
                        [
                          isDefaultProject ? "project-slug-description" : null,
                          error || update.isError ? "project-slug-error" : null,
                        ]
                          .filter(Boolean)
                          .join(" ") || undefined
                      }
                    />
                    {isDefaultProject && (
                      <FieldDescription id="project-slug-description">
                        The default project slug cannot be changed.
                      </FieldDescription>
                    )}
                    {(error || update.isError) && (
                      <FieldError id="project-slug-error">
                        {error ?? update.error?.message}
                      </FieldError>
                    )}
                  </Field>
                );
              }}
            </form.Field>
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
                  <RequireScope scope="project:write" level="component">
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
      <Dialog
        open={slugWarningOpen}
        onOpenChange={(open) => {
          if (update.isPending) return;
          setSlugWarningOpen(open);
          if (!open) setPendingSlugChange(null);
        }}
      >
        <Dialog.Content closeable={!update.isPending}>
          <Dialog.Header>
            <Dialog.Title>Change project slug?</Dialog.Title>
            <Dialog.Description>
              Changing the slug will break saved project links and integrations
              or CLI profiles configured with the current slug. You will need to
              update them to use <strong>{pendingSlugChange?.slug}</strong>.
            </Dialog.Description>
          </Dialog.Header>
          {update.isError && (
            <Alert variant="error" dismissible={false}>
              {update.error.message}
            </Alert>
          )}
          <Dialog.Footer>
            <Button
              variant="tertiary"
              disabled={update.isPending}
              onClick={() => {
                setSlugWarningOpen(false);
                setPendingSlugChange(null);
              }}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
            <Button
              variant="destructive-primary"
              disabled={update.isPending || !pendingSlugChange}
              onClick={() => {
                if (!pendingSlugChange) return;
                save(pendingSlugChange);
              }}
            >
              <Button.Text>
                {update.isPending ? "Changing…" : "Change slug"}
              </Button.Text>
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </SettingsSection>
  );
}
