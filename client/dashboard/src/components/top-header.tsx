import { useOrganization, useProject } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import { CheckIcon, ChevronsUpDown, PlusIcon } from "lucide-react";
import { useState } from "react";
import { GramLogo } from "./gram-logo";
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
import { Type } from "./ui/type";

export function TopHeader() {
  const routes = useRoutes();
  const organization = useOrganization();
  const project = useProject();
  const [open, setOpen] = useState(false);
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [newProjectName, setNewProjectName] = useState("");
  const client = useSdkClient();

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
      createProjectRequestBody: {
        name,
        organizationId: organization.id,
      },
    });
    setCreateDialogOpen(false);
    setNewProjectName("");
    project.switchProject(result.project.slug);
  };

  return (
    <>
      <header className="flex items-center h-14 pl-5 pr-4 border-b bg-white shrink-0">
        <div className="flex items-center gap-3">
          {/* Logo */}
          <routes.home.Link className="hover:no-underline flex items-center">
            <GramLogo className="w-20" />
          </routes.home.Link>

          {/* Separator */}
          <span className="text-muted-foreground/50 text-xl select-none">/</span>

          {/* Org/Project Switcher */}
          <Popover open={open} onOpenChange={setOpen}>
            <PopoverTrigger asChild>
              <Button variant="ghost" className="h-8 !px-2 gap-2 relative -left-1">
                <ProjectAvatar project={project} className="h-5 w-5 rounded shrink-0" />
                <span className="text-base font-medium">
                  {project?.slug ?? "Select"}
                </span>
                <ChevronsUpDown className="w-4 h-4 text-muted-foreground shrink-0" />
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
                    {organization.projects.map((p) => (
                      <CommandItem
                        key={p.id}
                        value={p.slug}
                        onSelect={() => handleProjectSelect(p.slug)}
                        className="flex items-center gap-2 cursor-pointer"
                      >
                        <ProjectAvatar project={p} className="h-5 w-5 rounded shrink-0" />
                        <span className="flex-1 truncate">{p.slug}</span>
                        {p.id === project.id && (
                          <CheckIcon className="w-4 h-4 shrink-0" />
                        )}
                      </CommandItem>
                    ))}
                  </CommandGroup>
                </CommandList>
              </Command>
              <button
                onClick={() => handleProjectSelect("new-project")}
                className="flex items-center gap-2 w-full px-3 py-2 text-sm cursor-pointer hover:bg-accent border-t"
              >
                <PlusIcon className="w-5 h-5 text-muted-foreground shrink-0" />
                <span>Create Project</span>
              </button>
            </PopoverContent>
          </Popover>
        </div>
      </header>
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
