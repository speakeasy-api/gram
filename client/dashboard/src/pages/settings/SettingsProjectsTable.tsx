import { Button, Column, Icon, Stack, Table } from "@speakeasy-api/moonshine";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useOrganization, useProject } from "@/contexts/Auth";
import { ProjectEntry } from "@gram/client/models/components";
import { useDeleteProjectMutation } from "@gram/client/react-query/deleteProject";
import {
  invalidateListProjects,
  useListProjectsSuspense,
} from "@gram/client/react-query/listProjects";
import { useQueryClient } from "@tanstack/react-query";
import { useRef, useState } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";

export function SettingsProjectsTable() {
  const organization = useOrganization();
  const currentProject = useProject();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const [projectToDelete, setProjectToDelete] = useState<ProjectEntry | null>(
    null,
  );
  const [confirmationInput, setConfirmationInput] = useState("");
  const isDeletingCurrentProject = useRef(false);

  const { data: projectsData } = useListProjectsSuspense({
    organizationId: organization.id,
  });

  const deleteProjectMutation = useDeleteProjectMutation({
    onSuccess: async () => {
      const shouldNavigate = isDeletingCurrentProject.current;

      setProjectToDelete(null);
      setConfirmationInput("");
      isDeletingCurrentProject.current = false;

      await invalidateListProjects(queryClient, [
        {
          organizationId: organization.id,
        },
      ]);

      toast.success("Project deleted successfully");

      if (shouldNavigate) {
        // Navigate to the default project after deleting the current project
        navigate(`/${organization.slug}/default/settings`);
      }
    },
    onError: (error) => {
      console.error("Failed to delete project:", error);
      toast.error("Failed to delete project");
      isDeletingCurrentProject.current = false;
    },
  });

  const handleCloseDeleteProjectDialog = () => {
    setProjectToDelete(null);
    setConfirmationInput("");
  };

  const handleDeleteProject = () => {
    if (!projectToDelete) return;

    // Track if we're deleting the current project for post-deletion navigation
    isDeletingCurrentProject.current =
      projectToDelete.slug === currentProject.slug;

    deleteProjectMutation.mutate({
      request: {
        id: projectToDelete.id,
      },
    });
  };

  const defaultProject = projectsData.projects.find(
    (p) => p.slug === "default",
  );

  const projectColumns: Column<ProjectEntry>[] = [
    {
      key: "name",
      header: "Name",
      width: "1fr",
      render: (project: ProjectEntry) => (
        <Type variant="body">{project.name}</Type>
      ),
    },
    {
      key: "slug",
      header: "Slug",
      width: "1fr",
      render: (project: ProjectEntry) => (
        <Type variant="body">{project.slug}</Type>
      ),
    },
    {
      key: "actions",
      header: "",
      width: "80px",
      render: (project: ProjectEntry) => {
        const isDefault = project.slug === defaultProject?.slug;

        const DeleteButton = () => (
          <Button
            variant="tertiary"
            size="sm"
            onClick={() => setProjectToDelete(project)}
            disabled={isDefault}
            className={isDefault ? "" : "hover:text-destructive"}
          >
            <Button.LeftIcon>
              <Icon name="trash-2" className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text className="sr-only">Delete project</Button.Text>
          </Button>
        );

        if (isDefault) {
          return (
            <SimpleTooltip tooltip="The default project cannot be deleted">
              <DeleteButton />
            </SimpleTooltip>
          );
        }

        return <DeleteButton />;
      },
    },
  ];

  return (
    <>
      <Stack direction="horizontal" justify="space-between" align="center">
        <Heading variant="h4">Projects</Heading>
      </Stack>
      <Table
        columns={projectColumns}
        data={projectsData?.projects ?? []}
        rowKey={(row) => row.id}
        className="min-h-fit max-h-[500px] overflow-y-auto"
        noResultsMessage={
          <Stack
            gap={2}
            className="h-full p-4 bg-background"
            align="center"
            justify="center"
          >
            <Type variant="body">No projects yet</Type>
          </Stack>
        }
      />

      <Dialog
        open={!!projectToDelete}
        onOpenChange={(open) => !open && handleCloseDeleteProjectDialog()}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Delete Project</Dialog.Title>
          </Dialog.Header>
          <div className="space-y-4 py-4">
            <Type variant="body">
              Are you sure you want to delete the project{" "}
              <code className="font-mono font-bold px-1 py-0.5 bg-muted rounded">
                {projectToDelete?.name}
              </code>
              ? This action cannot be undone.
            </Type>

            <div className="space-y-2">
              <Label htmlFor="confirm-project-name">
                Type the project name to confirm:
              </Label>
              <Input
                id="confirm-project-name"
                value={confirmationInput}
                onChange={setConfirmationInput}
                placeholder={projectToDelete?.name}
                autoComplete="off"
                autoFocus
              />
            </div>

            <div className="flex justify-end space-x-2">
              <Button
                variant="secondary"
                onClick={handleCloseDeleteProjectDialog}
                disabled={deleteProjectMutation.isPending}
              >
                Cancel
              </Button>
              <Button
                variant="destructive-primary"
                onClick={handleDeleteProject}
                disabled={
                  confirmationInput.trim() !== projectToDelete?.name ||
                  deleteProjectMutation.isPending
                }
              >
                {deleteProjectMutation.isPending
                  ? "Deleting..."
                  : "Delete Project"}
              </Button>
            </div>
          </div>
        </Dialog.Content>
      </Dialog>
    </>
  );
}
