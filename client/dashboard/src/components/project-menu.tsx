import { useOrganization, useProject, useSession } from "@/contexts/Auth.tsx";
import { useSdkClient } from "@/contexts/Sdk.tsx";
import { cn } from "@/lib/utils.ts";
import { ProjectEntry } from "@gram/client/models/components";
import { Icon, Stack } from "@speakeasy-api/moonshine";
import { ChevronsUpDown, PlusIcon } from "lucide-react";
import React from "react";
import { InputDialog } from "./input-dialog.tsx";
import { NavButton } from "./nav-menu.tsx";
import { Button } from "./ui/button.tsx";
import { Combobox } from "./ui/combobox.tsx";
import { Heading } from "./ui/heading.tsx";
import { Popover, PopoverContent, PopoverTrigger } from "./ui/popover.tsx";
import { Separator } from "./ui/separator.tsx";
import { Skeleton } from "./ui/skeleton.tsx";
import { ThemeToggle } from "./ui/theme-toggle.tsx";
import { Type } from "./ui/type.tsx";

// Generate colors from project label
function getProjectColors(label: string): {
  from: string;
  to: string;
  angle: number;
} {
  // FNV-1a hash function for better distribution
  const fnv1a = (str: string) => {
    let hash = 2166136261;
    for (let i = 0; i < str.length; i++) {
      hash ^= str.charCodeAt(i);
      hash +=
        (hash << 1) + (hash << 4) + (hash << 7) + (hash << 8) + (hash << 24);
    }
    return hash >>> 0;
  };

  const hash = fnv1a(label);

  // Generate four random-ish numbers from the hash for more variation
  const n1 = hash % 360;
  const n2 = (hash >> 8) % 360;
  const n3 = (hash >> 16) % 100;
  const n4 = (hash >> 24) % 360; // For gradient angle

  const hue1 = n1;
  const hue2 = (hue1 + n2) % 360;
  const saturation = Math.max(65, n3);
  const angle = n4;

  return {
    from: `hsl(${hue1}, ${saturation}%, 65%)`,
    to: `hsl(${hue2}, ${saturation}%, 60%)`,
    angle,
  };
}

export function ProjectAvatar({
  project,
  className,
}: {
  project: Pick<ProjectEntry, "id">;
  className?: string;
}) {
  const colors = getProjectColors(project.id);
  return (
    <div
      className={cn("h-6 w-6 rounded-full bg-gradient-to-br", className)}
      style={{
        backgroundImage: `linear-gradient(${colors.angle}deg, ${colors.from}, ${colors.to})`,
      }}
    />
  );
}

export function ProjectMenu() {
  const session = useSession();
  const organization = useOrganization();
  const project = useProject();
  const client = useSdkClient();

  const [open, setOpen] = React.useState(false);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="ghost"
          role="combobox"
          aria-expanded={open}
          className="w-full justify-between h-12 p-2"
        >
          <Stack direction={"horizontal"} gap={3} align="center">
            <ProjectAvatar project={project} className="h-8 w-8 rounded-md" />
            <Stack align="start">
              <Heading variant="h5" className="mb-[-2px] normal-case">
                {project?.slug ?? "Select Project"}
              </Heading>
              <Type variant="small" muted className="truncate max-w-[120px]">
                {organization?.slug}
              </Type>
            </Stack>
          </Stack>
          <ChevronsUpDown className="text-muted-foreground hover:text-foreground" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[200px] p-0">
        <div className="flex flex-col gap-2 p-2">
          <Stack gap={1}>
            <Type variant="small" className="px-2">
              {organization?.slug}
            </Type>
            <Type muted variant="small" className="px-2 truncate">
              {session.userEmail}
            </Type>
          </Stack>
          <ProjectSelector />
          <Separator className="my-2" />
          <Stack
            direction={"horizontal"}
            gap={2}
            align="center"
            justify="space-between"
            className="pl-2"
          >
            <Type variant="small" muted>
              Theme
            </Type>
            <ThemeToggle />
          </Stack>
          <NavButton
            title="Logout"
            Icon={() => <Icon name="log-out" />}
            onClick={async () => {
              await client.auth.logout();
              window.location.href = "/login";
              setOpen(false);
            }}
          />
        </div>
      </PopoverContent>
    </Popover>
  );
}

function ProjectSelector() {
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
      <ProjectAvatar project={project} className="h-4 w-4 min-w-4 min-h-4" />
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
        <div className="flex items-center gap-2 w-full">
          <ProjectAvatar
            project={selected}
            className="h-4 w-4 min-w-4 min-h-4"
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
            createProject(newProjectName);
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
