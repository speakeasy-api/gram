import { useOrganization, useProject } from "@/contexts/Auth.tsx";
import { useSdkClient } from "@/contexts/Sdk.tsx";
import { cn } from "@/lib/utils.ts";
import { ProjectEntry } from "@gram/client/models/components";
import { PlusIcon } from "lucide-react";
import React from "react";
import { InputDialog } from "./input-dialog.tsx";
import { Combobox } from "./ui/combobox.tsx";
import { Skeleton } from "./ui/skeleton.tsx";
import { Type } from "./ui/type.tsx";

import { getGradientColors } from "@/components/gradient-colors";

export function ProjectAvatar({
  project,
  className,
}: {
  project: Pick<ProjectEntry, "id">;
  className?: string;
}): React.JSX.Element {
  const colors = getGradientColors(project.id);
  return (
    <div
      className={cn("h-6 w-6 rounded-full bg-gradient-to-br", className)}
      style={{
        backgroundImage: `linear-gradient(${colors.angle}deg, ${colors.from}, ${colors.to})`,
      }}
    />
  );
}

export function ProjectSelector(): React.JSX.Element {
  const organization = useOrganization();
  const project = useProject();
  const client = useSdkClient();

  const [createDialogOpen, setCreateDialogOpen] = React.useState(false);
  const [newProjectName, setNewProjectName] = React.useState("");

  const projectWithIcons = organization.projects.map((project) => ({
    ...project,
    value: project.slug,
    label: project.slug,
    icon: (
      <ProjectAvatar project={project} className="h-4 min-h-4 w-4 min-w-4" />
    ),
  }));

  projectWithIcons.push({
    value: "new-project",
    label: "New Project",
    icon: <PlusIcon className="h-4 w-4" />,
    id: "new-project",
    name: "New Project",
    slug: "new-project",
  });

  if (projectWithIcons.length === 0) {
    return <Skeleton className="h-8 w-full" />;
  }

  const selected =
    projectWithIcons.find((p) => p.id === project.id) ?? projectWithIcons[0]!;

  const changeProject = (slug: string) => {
    if (slug === "new-project") {
      setCreateDialogOpen(true);
    } else {
      project.switchProject(slug);
    }
  };

  const createProject = async (name: string) => {
    const result = await client.projects.create({
      createProjectRequestBody: {
        name,
        organizationId: organization.id,
      },
    });
    setCreateDialogOpen(false);
    project.switchProject(result.project.slug);
  };

  return (
    <>
      <Combobox
        selected={selected}
        onSelectionChange={(value) => changeProject(value.value)}
        items={projectWithIcons ?? []}
      >
        <div className="flex w-full items-center gap-2">
          <ProjectAvatar
            project={selected}
            className="h-4 min-h-4 w-4 min-w-4"
          />
          <Type className="truncate" variant="small">
            {selected?.label}
          </Type>
        </div>
      </Combobox>
      {createDialogOpen && (
        <InputDialog
          open={createDialogOpen}
          onOpenChange={() => setCreateDialogOpen(false)}
          title="Create New Project"
          description="Create a new project to get started"
          onSubmit={() => {
            void createProject(newProjectName);
          }}
          inputs={[
            {
              label: "Name",
              value: newProjectName,
              onChange: setNewProjectName,
              placeholder: "New Project",
            },
          ]}
        />
      )}
    </>
  );
}
