import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useOrganization, useProject } from "@/contexts/Auth";
import { useDeleteProjectMutation } from "@gram/client/react-query/deleteProject";
import { invalidateListProjects } from "@gram/client/react-query/listProjects";
import { Button } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";

export function SettingsDangerZone() {
  const organization = useOrganization();
  const project = useProject();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const [isDeleteProjectDialogOpen, setIsDeleteProjectDialogOpen] =
    useState(false);
  const [deleteProjectConfirmation, setDeleteProjectConfirmation] =
    useState("");

  const isDefaultProject = project.slug === "default";

  const deleteProjectMutation = useDeleteProjectMutation({
    onSuccess: async () => {
      setIsDeleteProjectDialogOpen(false);
      setDeleteProjectConfirmation("");
      await invalidateListProjects(queryClient, [
        { organizationId: organization.id },
      ]);
      toast.success("Project deleted successfully");
      navigate(`/${organization.slug}/default/settings`);
    },
    onError: (error) => {
      console.error("Failed to delete project:", error);
      toast.error("Failed to delete project");
    },
  });

  const handleCloseDialog = () => {
    setIsDeleteProjectDialogOpen(false);
    setDeleteProjectConfirmation("");
  };

  return (
    <>
      <div className="border border-destructive/30 rounded-lg p-6 mt-8">
        <Type variant="subheading" className="text-destructive mb-1">
          Danger Zone
        </Type>
        <Type muted small className="mb-4">
          Permanently delete this project and all its data. This action cannot
          be undone.
        </Type>
        {isDefaultProject ? (
          <SimpleTooltip tooltip="The default project cannot be deleted">
            <Button variant="destructive-primary" size="md" disabled>
              <Button.LeftIcon>
                <Trash2 className="h-4 w-4" />
              </Button.LeftIcon>
              <Button.Text>Delete Project</Button.Text>
            </Button>
          </SimpleTooltip>
        ) : (
          <Button
            variant="destructive-primary"
            size="md"
            onClick={() => setIsDeleteProjectDialogOpen(true)}
          >
            <Button.LeftIcon>
              <Trash2 className="h-4 w-4" />
            </Button.LeftIcon>
            <Button.Text>Delete Project</Button.Text>
          </Button>
        )}
      </div>

      <Dialog open={isDeleteProjectDialogOpen} onOpenChange={handleCloseDialog}>
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>Delete Project</Dialog.Title>
          </Dialog.Header>
          <div className="space-y-4 py-4">
            <Type variant="body">
              Are you sure you want to delete the project{" "}
              <code className="font-mono font-bold px-1 py-0.5 bg-muted rounded">
                {project.name}
              </code>
              ? This action cannot be undone.
            </Type>

            <div className="space-y-2">
              <Label htmlFor="confirm-project-name">
                Type the project name to confirm:
              </Label>
              <Input
                id="confirm-project-name"
                value={deleteProjectConfirmation}
                onChange={setDeleteProjectConfirmation}
                placeholder={project.name}
                autoComplete="off"
                autoFocus
              />
            </div>

            <div className="flex justify-end space-x-2">
              <Button
                variant="secondary"
                onClick={handleCloseDialog}
                disabled={deleteProjectMutation.isPending}
              >
                Cancel
              </Button>
              <Button
                variant="destructive-primary"
                onClick={() => {
                  deleteProjectMutation.mutate({
                    request: { id: project.id },
                  });
                }}
                disabled={
                  deleteProjectConfirmation.trim() !== project.name ||
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
