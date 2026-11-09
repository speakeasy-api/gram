import { getGradientColors } from "@/components/gradient-colors";
import { useOrganization, useProject, useSession } from "@/contexts/Auth";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { cn } from "@/lib/utils";
import {
  ArrowLeftRight,
  CheckIcon,
  ChevronsUpDown,
  PlusIcon,
} from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router";
import { InputDialog } from "./input-dialog";
import { ProjectIcon } from "./project-icon";
import { Button } from "./ui/Button";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/Command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";

type OrgProject = ReturnType<typeof useOrganization>["projects"][number];

/**
 * Searchable list of the organization's projects. The session only carries
 * the projects the viewer can read, so every row is one they can open.
 */
function ProjectCommandList({
  projects,
  currentProjectId,
  onSelect,
}: {
  projects: readonly OrgProject[];
  currentProjectId?: string;
  onSelect: (slug: string) => void;
}): JSX.Element {
  return (
    // CommandInput's wrapper is a fixed h-14 (command-palette sizing);
    // override it here so the popover's search row stays compact.
    <Command className="border-none [&_[data-slot=command-input-wrapper]]:h-10">
      <CommandInput placeholder="Find Project..." className="h-10" />
      <CommandList className="max-h-[250px] !p-1">
        <CommandEmpty>No projects found.</CommandEmpty>
        <CommandGroup heading="Projects">
          {[...projects]
            .sort((a, b) => a.slug.localeCompare(b.slug))
            .map((p) => (
              <CommandItem
                key={p.id}
                value={p.slug}
                // Let search match the display name as well as the slug.
                keywords={p.name ? [p.name] : undefined}
                onSelect={() => onSelect(p.slug)}
                className="flex cursor-pointer items-center gap-2"
              >
                <ProjectIcon
                  decorative
                  project={p}
                  className="size-5 text-[10px]"
                />
                <span className="flex-1 truncate">{p.name || p.slug}</span>
                {/* Case-insensitive: "Default" / "default" is the same
                    name, so the slug adds nothing. */}
                {p.name && p.name.toLowerCase() !== p.slug.toLowerCase() && (
                  <span className="text-muted-foreground max-w-[80px] truncate font-mono text-xs">
                    {p.slug}
                  </span>
                )}
                {p.id === currentProjectId && (
                  <CheckIcon className="h-4 w-4 shrink-0" />
                )}
              </CommandItem>
            ))}
        </CommandGroup>
      </CommandList>
    </Command>
  );
}

function PopoverFooterButton({
  onClick,
  Icon,
  children,
}: {
  onClick: () => void;
  Icon: React.ComponentType<{ className?: string }>;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <button
      type="button"
      onClick={onClick}
      className="hover:bg-accent flex w-full cursor-pointer items-center gap-2 px-3 py-2 text-sm"
    >
      <Icon className="text-muted-foreground h-5 w-5 shrink-0" />
      <span>{children}</span>
    </button>
  );
}

/**
 * Org-level pages have no project, so the trigger names the organization.
 * It still opens the project list: a member whose access is limited to some
 * projects lands on org pages they can't view, and this is their way into
 * the projects they can.
 */
function OrgSwitcher({ className }: { className?: string }): JSX.Element {
  const organization = useOrganization();
  const session = useSession();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const isMultiOrg = session.organizations.length > 1;
  const projects = organization.projects;

  const orgLabel = organization.name || organization.slug;
  const orgColors = getGradientColors(organization.id);
  const orgInitial = orgLabel.charAt(0).toUpperCase();

  // Collapsed, swap the truncated name for a gradient initial tile (mirrors
  // ProjectIcon).
  const content = (
    <>
      <div
        className={cn(
          "flex shrink-0 items-center justify-center transition-shadow",
          "group-data-[collapsible=icon]:ring-border/50 group-data-[collapsible=icon]:bg-card group-data-[collapsible=icon]:p-1 group-data-[collapsible=icon]:ring-1",
        )}
      >
        <div
          aria-label={orgLabel}
          className="flex size-6 shrink-0 items-center justify-center bg-gradient-to-br text-xs font-semibold text-white group-data-[collapsible=icon]:size-7 group-data-[collapsible=icon]:text-[14px]"
          style={{
            backgroundImage: `linear-gradient(${orgColors.angle}deg, ${orgColors.from}, ${orgColors.to})`,
          }}
        >
          {orgInitial}
        </div>
      </div>
      <span className="truncate text-sm font-medium group-data-[collapsible=icon]:hidden">
        {orgLabel}
      </span>
    </>
  );

  const rowClass = cn(
    "flex w-full items-center gap-2 border px-2 py-1.5 text-sm font-medium group-data-[collapsible=icon]:w-auto group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:border-0 group-data-[collapsible=icon]:p-0",
    className,
  );

  if (projects.length === 0 && !isMultiOrg) {
    return <div className={rowClass}>{content}</div>;
  }

  const goTo = (path: string) => {
    setOpen(false);
    void navigate(path);
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          variant="tertiary"
          className={cn(
            "hover:bg-accent h-auto w-full justify-start gap-2 border px-2 py-1.5 font-sans group-data-[collapsible=icon]:w-auto group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-1",
            className,
          )}
        >
          {content}
          <ChevronsUpDown className="text-muted-foreground ml-auto h-4 w-4 shrink-0 group-data-[collapsible=icon]:hidden" />
        </Button>
      </PopoverTrigger>
      <PopoverContent className="w-[240px] p-0" align="start">
        {projects.length > 0 ? (
          <ProjectCommandList
            projects={projects}
            onSelect={(slug) => goTo(`/${organization.slug}/projects/${slug}`)}
          />
        ) : (
          <p className="text-muted-foreground px-3 py-3 text-sm">
            You're not in any projects in {orgLabel} yet. Ask an admin to add
            you to one.
          </p>
        )}
        {isMultiOrg && (
          <>
            <div className="bg-border h-px" />
            <PopoverFooterButton
              onClick={() => goTo("/switch-org")}
              Icon={ArrowLeftRight}
            >
              Switch organization
            </PopoverFooterButton>
          </>
        )}
      </PopoverContent>
    </Popover>
  );
}

export function WorkspaceSwitcher({
  className,
}: {
  className?: string;
} = {}): JSX.Element {
  const { projectSlug } = useSlugs();
  if (!projectSlug) return <OrgSwitcher className={className} />;
  return <ProjectSwitcher className={className} />;
}

function ProjectSwitcher({ className }: { className?: string }): JSX.Element {
  const organization = useOrganization();
  const project = useProject();
  const { projectSlug } = useSlugs();
  const client = useSdkClient();
  const [open, setOpen] = useState(false);
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [newProjectName, setNewProjectName] = useState("");

  const createProject = async (name: string) => {
    const result = await client.projects.create({
      createProjectRequestBody: { name, organizationId: organization.id },
    });
    setCreateDialogOpen(false);
    setNewProjectName("");
    project.switchProject(result.project.slug);
  };

  return (
    <>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button
            variant="tertiary"
            className={cn(
              "hover:bg-accent h-auto w-full justify-start gap-2 border px-2 py-1.5 font-sans group-data-[collapsible=icon]:w-auto group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-1",
              className,
            )}
          >
            <ProjectIcon
              decorative
              project={project}
              className="size-5 text-[10px]"
            />
            {/* "Project" suffix names what the switcher switches. */}
            <span className="truncate text-sm font-medium group-data-[collapsible=icon]:hidden">
              {project?.name || project?.slug || projectSlug} Project
            </span>
            <ChevronsUpDown className="text-muted-foreground ml-auto h-4 w-4 shrink-0 group-data-[collapsible=icon]:hidden" />
          </Button>
        </PopoverTrigger>
        <PopoverContent className="w-[240px] p-0" align="start">
          <ProjectCommandList
            projects={organization.projects}
            currentProjectId={project.id}
            onSelect={(slug) => {
              setOpen(false);
              project.switchProject(slug);
            }}
          />
          <div className="bg-border h-px" />
          <PopoverFooterButton
            onClick={() => {
              setOpen(false);
              setCreateDialogOpen(true);
            }}
            Icon={PlusIcon}
          >
            Create Project
          </PopoverFooterButton>
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
