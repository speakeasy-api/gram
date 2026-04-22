import {
  useIsAdmin,
  useOrganization,
  useProject,
  useUser,
} from "@/contexts/Auth";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { useOrgRoutes, useRoutes } from "@/routes";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
  ThemeSwitcher,
} from "@speakeasy-api/moonshine";
import {
  BugIcon,
  CheckIcon,
  ChevronsUpDown,
  ArrowRightLeftIcon,
  CreditCardIcon,
  LogOutIcon,
  MailIcon,
  MessageCircleIcon,
  PlusIcon,
  SettingsIcon,
} from "lucide-react";
import { useCallback, useState } from "react";
import { Link } from "react-router";
import { useRBAC } from "@/hooks/useRBAC";
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
  const orgRoutes = useOrgRoutes();
  const organization = useOrganization();
  const project = useProject();
  const user = useUser();
  const { projectSlug } = useSlugs();
  const [open, setOpen] = useState(false);
  const isAdmin = useIsAdmin();
  const { hasAnyScope } = useRBAC();
  const canAccessOrgRoutes = hasAnyScope(["org:read", "org:admin"]);
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [pylonOpen, setPylonOpen] = useState(false);
  const togglePylon = useCallback(() => {
    if (pylonOpen) {
      window.Pylon?.("hide");
    } else {
      window.Pylon?.("show");
    }
    setPylonOpen((prev) => !prev);
  }, [pylonOpen]);
  const [newProjectName, setNewProjectName] = useState("");
  const client = useSdkClient();

  const userInitials =
    user.displayName
      ?.split(" ")
      .map((n) => n[0])
      .join("")
      .toUpperCase()
      .slice(0, 2) ||
    user.email?.slice(0, 2).toUpperCase() ||
    "?";

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
      name,
      organizationId: organization.id,
    });
    setCreateDialogOpen(false);
    setNewProjectName("");
    project.switchProject(result.project.slug);
  };

  return (
    <>
      <header className="dark:bg-background flex h-14 shrink-0 items-center border-b bg-white pr-4 pl-5">
        <div className="flex items-center gap-3">
          {/* Logo */}
          <Link
            to={projectSlug ? routes.home.href() : `/${organization.slug}`}
            className="flex items-center hover:no-underline"
          >
            <GramLogo className="w-28" />
          </Link>

          {/* Separator */}
          <span className="text-muted-foreground/50 text-xl select-none">
            /
          </span>

          {/* Org link */}
          {canAccessOrgRoutes ? (
            <Link
              to={`/${organization.slug}`}
              className="text-foreground/80 hover:text-foreground hover:bg-accent rounded-md px-2 py-1 text-base font-medium whitespace-nowrap transition-colors hover:no-underline"
            >
              {organization.slug}
            </Link>
          ) : (
            <span className="text-foreground/80 cursor-default rounded-md px-2 py-1 text-base font-medium whitespace-nowrap">
              {organization.slug}
            </span>
          )}

          {/* Project Switcher - hidden on org-level pages */}
          {projectSlug && (
            <>
              <span className="text-muted-foreground/50 text-xl select-none">
                /
              </span>
              <Popover open={open} onOpenChange={setOpen}>
                <PopoverTrigger asChild>
                  <Button
                    variant="ghost"
                    className="relative -left-1 h-8 gap-2 !px-2"
                  >
                    <ProjectAvatar
                      project={project}
                      className="h-5 w-5 shrink-0 rounded"
                    />
                    <span className="text-base font-medium">
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
        </div>

        {/* Right side - Nav links, Theme toggle & User menu */}
        <div className="ml-auto flex items-center gap-4">
          <nav className="hidden items-center gap-2 lg:flex">
            {"Pylon" in window && (
              <Button
                variant="default"
                size="sm"
                className="text-sm"
                onClick={togglePylon}
              >
                {pylonOpen ? "Close Support" : "Get Support"}
              </Button>
            )}
            <Button variant="outline" size="sm" className="text-sm" asChild>
              <a
                href="https://www.speakeasy.com/docs/mcp"
                target="_blank"
                rel="noopener noreferrer"
              >
                Docs
              </a>
            </Button>
            <Button variant="outline" size="sm" className="text-sm" asChild>
              <a
                href="https://www.speakeasy.com/changelog?product=mcp-platform"
                target="_blank"
                rel="noopener noreferrer"
              >
                Changelog
              </a>
            </Button>
          </nav>
          <ThemeSwitcher />
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                variant="ghost"
                className="size-9 rounded-full border border-[#dbdbdb] p-0 dark:border-[#333]/30"
              >
                <Avatar className="size-9">
                  <AvatarImage
                    src={user.photoUrl}
                    alt={user.displayName || user.email}
                  />
                  <AvatarFallback className="text-xs">
                    {userInitials}
                  </AvatarFallback>
                </Avatar>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-56">
              <DropdownMenuLabel className="font-normal">
                <div className="flex flex-col space-y-1">
                  <p className="text-sm leading-none font-medium">
                    {user.displayName || "User"}
                  </p>
                  <p className="text-muted-foreground text-xs leading-none">
                    {user.email}
                  </p>
                </div>
              </DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                {projectSlug && (
                  <DropdownMenuItem onClick={() => routes.settings.goTo()}>
                    <SettingsIcon className="mr-2 h-4 w-4" />
                    Project Settings
                  </DropdownMenuItem>
                )}
                {canAccessOrgRoutes && (
                  <DropdownMenuItem onClick={() => orgRoutes.billing.goTo()}>
                    <CreditCardIcon className="mr-2 h-4 w-4" />
                    Billing
                  </DropdownMenuItem>
                )}
                {isAdmin && (
                  <DropdownMenuItem
                    onClick={() => orgRoutes.adminSettings.goTo()}
                  >
                    <ArrowRightLeftIcon className="mr-2 h-4 w-4" />
                    Organization Override
                  </DropdownMenuItem>
                )}
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                {"Pylon" in window && (
                  <DropdownMenuItem onClick={togglePylon}>
                    <MessageCircleIcon className="mr-2 h-4 w-4" />
                    {pylonOpen ? "Close Support" : "Get Support"}
                  </DropdownMenuItem>
                )}
                <DropdownMenuItem asChild>
                  <a href="mailto:gram@speakeasy.com">
                    <MailIcon className="mr-2 h-4 w-4" />
                    Email Team
                  </a>
                </DropdownMenuItem>
                <DropdownMenuItem asChild>
                  <a
                    href="https://github.com/speakeasy-api/gram/issues/new"
                    target="_blank"
                    rel="noopener noreferrer"
                  >
                    <BugIcon className="mr-2 h-4 w-4" />
                    Bug or Feature Request
                  </a>
                </DropdownMenuItem>
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                onClick={async () => {
                  await client.auth.logout();
                  window.location.href = "/login";
                }}
              >
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
