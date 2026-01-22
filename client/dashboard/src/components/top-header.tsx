import { useOrganization, useProject, useUser } from "@/contexts/Auth";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { useRoutes } from "@/routes";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@speakeasy-api/moonshine";
import { ThemeSwitcher } from "@speakeasy-api/moonshine";
import { CheckIcon, ChevronsUpDown, LogOutIcon, PlusIcon, SettingsIcon, CreditCardIcon } from "lucide-react";
import { useState } from "react";
import { GramLogo } from "./gram-logo";
import { InputDialog } from "./input-dialog";
import { ProjectAvatar } from "./project-menu";
import { Avatar, AvatarFallback, AvatarImage } from "./ui/avatar";
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

export function TopHeader() {
  const routes = useRoutes();
  const organization = useOrganization();
  const project = useProject();
  const user = useUser();
  const { projectSlug } = useSlugs();
  const [open, setOpen] = useState(false);
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [newProjectName, setNewProjectName] = useState("");
  const client = useSdkClient();

  const userInitials = user.displayName
    ?.split(" ")
    .map((n) => n[0])
    .join("")
    .toUpperCase()
    .slice(0, 2) || user.email?.slice(0, 2).toUpperCase() || "?";

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
      <header className="flex items-center h-14 pl-5 pr-4 border-b bg-white dark:bg-background shrink-0">
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
                  {project?.slug || projectSlug || "Select"}
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

        {/* Right side - Theme toggle & User menu */}
        <div className="ml-auto flex items-center gap-4">
          <ThemeSwitcher />
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" className="h-8 w-8 rounded-full p-0">
                <Avatar className="h-8 w-8">
                  <AvatarImage src={user.photoUrl} alt={user.displayName || user.email} />
                  <AvatarFallback className="text-xs">{userInitials}</AvatarFallback>
                </Avatar>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-56">
              <DropdownMenuLabel className="font-normal">
                <div className="flex flex-col space-y-1">
                  <p className="text-sm font-medium leading-none">{user.displayName || "User"}</p>
                  <p className="text-xs leading-none text-muted-foreground">{user.email}</p>
                </div>
              </DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuItem onClick={() => routes.settings.goTo()}>
                  <SettingsIcon className="mr-2 h-4 w-4" />
                  Settings
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => routes.billing.goTo()}>
                  <CreditCardIcon className="mr-2 h-4 w-4" />
                  Billing
                </DropdownMenuItem>
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={() => window.location.href = "/logout"}>
                <LogOutIcon className="mr-2 h-4 w-4" />
                Log out
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
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
