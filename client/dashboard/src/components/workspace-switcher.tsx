import { getGradientColors } from "@/components/gradient-colors";
import { useOrganization, useProject, useSession } from "@/contexts/Auth";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { cn } from "@/lib/utils";
import { CheckIcon, ChevronDown, ChevronsUpDown, PlusIcon } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router";
import { InputDialog } from "./input-dialog";
import { ProjectAvatar } from "./project-menu";
import { Button } from "./ui/moonshine";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "./ui/command";
import { Popover, PopoverContent, PopoverTrigger } from "./ui/popover";

export function WorkspaceSwitcher(): JSX.Element {
  const organization = useOrganization();
  const project = useProject();
  const session = useSession();
  const navigate = useNavigate();
  const { projectSlug } = useSlugs();
  const client = useSdkClient();
  const isMultiOrg = session.organizations.length > 1;
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

  // Org-level pages have no project — render org context only. Collapsed,
  // swap the truncated name for a gradient initial tile (mirrors ProjectAvatar).
  // With multiple orgs the row becomes a button that opens the org switcher.
  if (!projectSlug) {
    const orgLabel = organization.name || organization.slug;
    const orgColors = getGradientColors(organization.id);
    const orgInitial = orgLabel.charAt(0).toUpperCase();
    const rowClass =
      "flex w-full items-center gap-2 rounded-md border px-2 py-1.5 text-sm font-medium group-data-[collapsible=icon]:w-auto group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:border-0 group-data-[collapsible=icon]:p-0";
    const content = (
      <>
        <div
          className={cn(
            "flex shrink-0 items-center justify-center transition-shadow",
            "group-data-[collapsible=icon]:ring-border/50 group-data-[collapsible=icon]:bg-card group-data-[collapsible=icon]:rounded-lg group-data-[collapsible=icon]:p-1 group-data-[collapsible=icon]:ring-1",
            isMultiOrg &&
              "group-data-[collapsible=icon]:hover:ring-foreground/15 group-data-[collapsible=icon]:hover:ring-2",
          )}
        >
          <div
            aria-label={orgLabel}
            className="flex size-6 shrink-0 items-center justify-center rounded-md bg-gradient-to-br text-xs font-semibold text-white group-data-[collapsible=icon]:size-7 group-data-[collapsible=icon]:text-[14px]"
            style={{
              backgroundImage: `linear-gradient(${orgColors.angle}deg, ${orgColors.from}, ${orgColors.to})`,
            }}
          >
            {orgInitial}
          </div>
        </div>
        <span className="truncate group-data-[collapsible=icon]:hidden">
          {orgLabel}
        </span>
        {isMultiOrg && (
          <ChevronDown className="text-muted-foreground ml-auto h-4 w-4 shrink-0 group-data-[collapsible=icon]:hidden" />
        )}
      </>
    );

    if (!isMultiOrg) {
      return <div className={rowClass}>{content}</div>;
    }

    return (
      <button
        type="button"
        onClick={() => void navigate("/switch-org")}
        className={cn(
          rowClass,
          "hover:bg-accent cursor-pointer text-left transition-colors group-data-[collapsible=icon]:hover:bg-transparent",
        )}
      >
        {content}
      </button>
    );
  }

  return (
    <>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button
            variant="tertiary"
            className="h-auto w-full justify-start gap-2 rounded-md border px-2 py-1.5 group-data-[collapsible=icon]:w-auto group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-1"
          >
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
            <CommandInput placeholder="Find Project..." className="h-10" />
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
          <div className="bg-border h-px" />
          <button
            onClick={() => handleProjectSelect("new-project")}
            className="hover:bg-accent flex w-full cursor-pointer items-center gap-2 px-3 py-2 text-sm"
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
