import { useOrganization, useProject } from "@/contexts/Auth";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { CheckIcon, ChevronsUpDown, PlusIcon } from "lucide-react";
import { useState } from "react";
import { BrandGradientRail } from "./brand-gradient-rail";
import { InputDialog } from "./input-dialog";
import { ProjectAvatar } from "./project-menu";
import { Button } from "./ui/button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "./ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "./ui/popover";

export function WorkspaceSwitcher() {
  const organization = useOrganization();
  const project = useProject();
  const { projectSlug } = useSlugs();
  const client = useSdkClient();
  const [open, setOpen] = useState(false);
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [newProjectName, setNewProjectName] = useState("");

  const handleProjectSelect = (slug: string) => {
    if (slug === "new-project") {
      setCreateDialogOpen(true);
    } else {
      project.switchProject(slug);
    }
    setOpen(false);
  };

  const createProject = async (name: string) => {
    const result = await client.projects.create({
      createProjectRequestBody: { name, organizationId: organization.id },
    });
    setCreateDialogOpen(false);
    setNewProjectName("");
    project.switchProject(result.project.slug);
  };

  // Org-level pages have no project — render org context only.
  if (!projectSlug) {
    return (
      <div className="relative flex items-center gap-2 overflow-hidden rounded-md border px-2 py-1.5 text-sm font-medium">
        <BrandGradientRail className="absolute top-0 bottom-0 left-0 rounded-none" />
        <span className="truncate">
          {organization.name || organization.slug}
        </span>
      </div>
    );
  }

  return (
    <>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button
            variant="ghost"
            className="relative h-auto w-full justify-start gap-2 overflow-hidden rounded-md border px-2 py-1.5 group-data-[collapsible=icon]:w-auto group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-1"
          >
            <BrandGradientRail className="absolute top-0 bottom-0 left-0 rounded-none" />
            <ProjectAvatar
              project={project}
              className="h-5 w-5 shrink-0 rounded"
            />
            <span className="truncate text-sm font-medium group-data-[collapsible=icon]:hidden">
              {project?.name || project?.slug || projectSlug}
            </span>
            <ChevronsUpDown className="text-muted-foreground ml-auto h-4 w-4 shrink-0 group-data-[collapsible=icon]:hidden" />
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-[240px] p-0" align="start">
          <Command className="border-none">
            <div className="border-b">
              <CommandInput placeholder="Find Project..." className="h-10" />
            </div>
            <CommandList className="max-h-[250px] !p-1">
              <CommandEmpty>No projects found.</CommandEmpty>
              <CommandGroup heading="Projects">
                {[...organization.projects]
                  .sort((a, b) => a.slug.localeCompare(b.slug))
                  .map((p) => (
                    <CommandItem
                      key={p.id}
                      value={p.slug}
                      onSelect={() => handleProjectSelect(p.slug)}
                      className="flex cursor-pointer items-center gap-2"
                    >
                      <ProjectAvatar
                        project={p}
                        className="h-5 w-5 shrink-0 rounded"
                      />
                      <span className="flex-1 truncate">{p.slug}</span>
                      {p.id === project.id && (
                        <CheckIcon className="h-4 w-4 shrink-0" />
                      )}
                    </CommandItem>
                  ))}
              </CommandGroup>
            </CommandList>
          </Command>
          <button
            onClick={() => handleProjectSelect("new-project")}
            className="hover:bg-accent flex w-full cursor-pointer items-center gap-2 border-t px-3 py-2 text-sm"
          >
            <PlusIcon className="text-muted-foreground h-5 w-5 shrink-0" />
            <span>Create Project</span>
          </button>
        </PopoverContent>
      </Popover>
      {createDialogOpen && (
        <InputDialog
          open={createDialogOpen}
          onOpenChange={() => {
            setCreateDialogOpen(false);
            setNewProjectName("");
          }}
          title="Create New Project"
          description="Create a new project to get started"
          onSubmit={() => createProject(newProjectName)}
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
