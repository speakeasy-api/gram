import { Dialog } from "@/components/ui/dialog";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { useOrganization } from "@/contexts/Auth";
import { Stack } from "@speakeasy-api/moonshine";
import { ArrowRight, Check, FolderOpen, Loader2 } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router";
import { useInstallCollection } from "./hooks";
import type { Collection } from "./types";

interface InstallCollectionDialogProps {
  collection: Collection;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function InstallCollectionDialog({
  collection,
  open,
  onOpenChange,
}: InstallCollectionDialogProps) {
  const organization = useOrganization();
  const navigate = useNavigate();
  const [selectedProjectId, setSelectedProjectId] = useState<string | null>(
    null,
  );
  const installMutation = useInstallCollection();

  const projects = organization.projects ?? [];
  const selectedProject = projects.find((p) => p.id === selectedProjectId);

  const handleInstall = () => {
    if (!selectedProjectId || !selectedProject?.slug || !collection.slug)
      return;
    installMutation.mutate({
      collectionId: collection.id,
      collectionSlug: collection.slug,
      projectSlug: selectedProject.slug,
    });
  };

  const handleClose = (nextOpen: boolean) => {
    if (!nextOpen) {
      installMutation.reset();
      setSelectedProjectId(null);
    }
    onOpenChange(nextOpen);
  };

  const isPending = installMutation.isPending;
  const isSuccess = installMutation.isSuccess;

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <Dialog.Content className="sm:max-w-lg">
        <Dialog.Header>
          <Dialog.Title>
            {isSuccess ? "Installed!" : `Install ${collection.name}`}
          </Dialog.Title>
          {!isSuccess && (
            <Dialog.Description>
              Choose a project to install {collection.servers.length} MCP{" "}
              {collection.servers.length === 1 ? "server" : "servers"} into.
            </Dialog.Description>
          )}
        </Dialog.Header>

        {isSuccess ? (
          <Stack gap={4} align="center" className="py-6">
            <div className="w-12 h-12 rounded-full bg-green-500/10 flex items-center justify-center">
              <Check className="w-6 h-6 text-green-500" />
            </div>
            <Stack gap={1} align="center">
              <Type variant="subheading">Collection installed</Type>
              <Type small muted className="text-center">
                {collection.name} has been installed to{" "}
                <span className="font-medium text-foreground">
                  {selectedProject?.name ?? "your project"}
                </span>{" "}
                with {collection.servers.length} MCP servers.
              </Type>
            </Stack>
            {selectedProject?.slug && (
              <Button
                variant="secondary"
                onClick={() => {
                  handleClose(false);
                  navigate(
                    `/${organization.slug}/projects/${selectedProject.slug}/mcp`,
                  );
                }}
              >
                Go to MCP
                <ArrowRight className="w-4 h-4 ml-2" />
              </Button>
            )}
            <Button variant="ghost" onClick={() => handleClose(false)}>
              Done
            </Button>
          </Stack>
        ) : (
          <>
            {/* Project picker */}
            <Stack gap={2} className="my-2">
              <Type small muted className="font-medium">
                {organization.name}
              </Type>
              <div className="max-h-[240px] overflow-y-auto space-y-2 pr-1">
                {projects.length === 0 ? (
                  <div className="flex flex-col items-center justify-center py-8 text-center">
                    <FolderOpen className="w-8 h-8 text-muted-foreground mb-2" />
                    <Type small muted>
                      No projects found in this organization.
                    </Type>
                  </div>
                ) : (
                  projects.map((project) => (
                    <button
                      key={project.id}
                      onClick={() => setSelectedProjectId(project.id)}
                      className={cn(
                        "flex items-center gap-3 w-full p-3 rounded-lg border text-left transition-all",
                        selectedProjectId === project.id
                          ? "border-primary bg-primary/5 ring-1 ring-primary/20"
                          : "border-border hover:border-foreground/30",
                      )}
                    >
                      <FolderOpen
                        className={cn(
                          "w-4 h-4 shrink-0",
                          selectedProjectId === project.id
                            ? "text-primary"
                            : "text-muted-foreground",
                        )}
                      />
                      <Type
                        variant="subheading"
                        as="div"
                        className="text-sm flex-1 truncate"
                      >
                        {project.name}
                      </Type>
                      <div
                        className={cn(
                          "size-5 rounded-full border-2 flex items-center justify-center shrink-0 transition-colors",
                          selectedProjectId === project.id
                            ? "border-[#1DA1F2] bg-[#1DA1F2]"
                            : "border-muted-foreground/30",
                        )}
                      >
                        {selectedProjectId === project.id && (
                          <Check
                            className="size-3 text-white"
                            strokeWidth={3}
                          />
                        )}
                      </div>
                    </button>
                  ))
                )}
              </div>
            </Stack>

            <Dialog.Footer>
              <Button variant="ghost" onClick={() => handleClose(false)}>
                Cancel
              </Button>
              <Button
                onClick={handleInstall}
                disabled={!selectedProjectId || isPending}
              >
                {isPending ? (
                  <>
                    <Loader2 className="w-4 h-4 animate-spin mr-2" />
                    Installing...
                  </>
                ) : (
                  "Install"
                )}
              </Button>
            </Dialog.Footer>
          </>
        )}
      </Dialog.Content>
    </Dialog>
  );
}
