import { useState } from "react";
import { Link } from "react-router";

import { useOrganization, useProject } from "@/contexts/Auth";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
import { CheckIcon, ChevronsUpDown, PlusIcon } from "lucide-react";

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
import { SidebarTrigger } from "./ui/sidebar";

/**
 * Slim top strip that sits only over the content column. Org/logo/user
 * chrome moved into the sidebar (SidebarBrand + SidebarUserMenu); what
 * remains is per-page context: sidebar trigger, breadcrumb anchor, project
 * switcher, and right-side external links.
 */
export function TopHeader() {
  const organization = useOrganization();
  const project = useProject();
  const { projectSlug } = useSlugs();
  const [open, setOpen] = useState(false);
  const { hasAnyScope } = useRBAC();
  const canAccessOrgRoutes = hasAnyScope(["org:read", "org:admin"]);
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
      <header className="dark:bg-background flex h-12 shrink-0 items-center gap-2 border-b bg-white px-3">
        <SidebarTrigger className="text-muted-foreground hover:text-foreground" />

        {canAccessOrgRoutes ? (
          <Link
            to={`/${organization.slug}`}
            className="text-foreground/80 hover:text-foreground hover:bg-accent rounded-md px-2 py-1 text-sm font-medium whitespace-nowrap transition-colors hover:no-underline"
          >
            {organization.slug}
          </Link>
        ) : (
          <span className="text-foreground/80 cursor-default rounded-md px-2 py-1 text-sm font-medium whitespace-nowrap">
            {organization.slug}
          </span>
        )}

        {projectSlug && (
          <>
            <span className="text-muted-foreground/50 text-base select-none">
              /
            </span>
            <Popover open={open} onOpenChange={setOpen}>
              <PopoverTrigger asChild>
                <Button variant="ghost" className="h-8 gap-2 !px-2">
                  <ProjectAvatar
                    project={project}
                    className="h-5 w-5 shrink-0 rounded"
                  />
                  <span className="text-sm font-medium">
                    {project?.slug || projectSlug || "Select"}
                  </span>
                  <ChevronsUpDown className="text-muted-foreground h-4 w-4 shrink-0" />
                </Button>
              </PopoverTrigger>
              <PopoverContent className="w-[240px] p-0" align="start">
                <Command className="border-none">
                  <div className="border-b">
                    <CommandInput
                      placeholder="Find Project..."
                      className="h-10"
                    />
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
          </>
        )}

        <div className="ml-auto flex items-center gap-2">
          <Button variant="outline" size="sm" className="text-xs" asChild>
            <a
              href="https://www.speakeasy.com/docs/mcp"
              target="_blank"
              rel="noopener noreferrer"
            >
              Docs
            </a>
          </Button>
          <Button variant="outline" size="sm" className="text-xs" asChild>
            <a
              href="https://www.speakeasy.com/changelog?product=mcp-platform"
              target="_blank"
              rel="noopener noreferrer"
            >
              Changelog
            </a>
          </Button>
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
