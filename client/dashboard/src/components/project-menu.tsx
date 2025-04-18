import React from "react";
import { Combobox } from "./ui/combobox.tsx";
import { Heading } from "./ui/heading.tsx";
import { ChevronsUpDown } from "lucide-react";
import { Popover, PopoverContent, PopoverTrigger } from "./ui/popover.tsx";
import { Button } from "./ui/button.tsx";
import { IconLogout } from "@tabler/icons-react";
import { Type } from "./ui/type.tsx";
import { NavButton } from "./nav-menu.tsx";
import { Stack } from "@speakeasy-api/moonshine";
import { cn } from "@/lib/utils.ts";
import { ThemeToggle } from "./ui/theme-toggle.tsx";
import { useOrganization, useProject, useSession } from "@/contexts/Auth.tsx";
import { Separator } from "./ui/separator.tsx";
import { Project } from "@gram/client/models/components";
import { useLogoutMutation } from "@gram/client/react-query";

// Add this helper function to generate colors from project label
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

function ProjectAvatar({
  project,
  className,
}: {
  project: Project;
  className?: string;
}) {
  const colors = getProjectColors(project.projectId);
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
  const logoutMutation = useLogoutMutation();

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
                {organization?.organizationSlug}
              </Heading>
              <Type variant="small" muted className="truncate max-w-[120px]">
                {project?.projectSlug ?? "Select Project"}
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
              {organization?.organizationSlug}
            </Type>
            <Type muted variant="small" className="px-2">
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
            item={{
              title: "Logout",
              icon: IconLogout,
              onClick: async () => {
                await logoutMutation.mutateAsync({
                  security: {
                    sessionHeaderGramSession: "",
                  },
                });
                window.location.href = "/login";
                setOpen(false);
              },
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

  const projectWithIcons = organization?.projects.map((project) => ({
    ...project,
    value: project.projectId,
    label: project.projectSlug,
    icon: <ProjectAvatar project={project} className="h-4 w-4" />,
  }));

  // TODO: Removing new project icon until we need a flow for this
  // projectWithIcons?.push({
  //   value: "new-project",
  //   label: "New Project",
  //   icon: <PlusIcon className="h-4 w-4" />,
  //   projectId: "new-project",
  //   projectName: "New Project",
  //   projectSlug: "new-project",
  // });

  const selected = projectWithIcons?.find(
    (p) => p.projectId === project.projectId
  );

  const changeProject = (projectId: string) => {
    if (projectId === "new-project") {
      // TODO: Create new project
      console.log("new project");
    } else {
      project.switchProject(projectId);
    }
  };

  return (
    <Combobox
      selected={selected}
      onSelectionChange={(value) => changeProject(value.value)}
      items={projectWithIcons ?? []}
    >
      <Stack direction={"horizontal"} gap={2} align="center">
        <ProjectAvatar project={project} className="h-4 w-4" />
        <span className="truncate">{selected?.label}</span>
      </Stack>
      <ChevronsUpDown className="opacity-50" />
    </Combobox>
  );
}
