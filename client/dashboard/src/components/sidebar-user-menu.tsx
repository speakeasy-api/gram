import { useIsAdmin, useSession, useUser } from "@/contexts/Auth";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { useRBAC } from "@/hooks/useRBAC";
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
  ArrowRightLeftIcon,
  BookOpenIcon,
  BuildingIcon,
  CreditCardIcon,
  LogOutIcon,
  MailIcon,
  MapIcon,
  MessageCircleIcon,
  MoreHorizontal,
  PencilIcon,
  SettingsIcon,
} from "lucide-react";
import { useCallback, useState } from "react";
import { useNavigate } from "react-router";
import { Avatar, AvatarFallback, AvatarImage } from "./ui/avatar";

export function SidebarUserMenu() {
  const user = useUser();
  const session = useSession();
  const isAdmin = useIsAdmin();
  const navigate = useNavigate();
  const routes = useRoutes();
  const orgRoutes = useOrgRoutes();
  const client = useSdkClient();
  const { projectSlug } = useSlugs();
  const { hasAnyScope } = useRBAC();

  const canAccessOrgRoutes = hasAnyScope(["org:read", "org:admin"]);
  const isMultiOrg = session.organizations.length > 1;

  const [menuOpen, setMenuOpen] = useState(false);
  const [pylonOpen, setPylonOpen] = useState(false);
  const togglePylon = useCallback(() => {
    if (pylonOpen) {
      window.Pylon?.("hide");
    } else {
      window.Pylon?.("show");
    }
    setPylonOpen((prev) => !prev);
  }, [pylonOpen]);

  const userInitials =
    user.displayName
      ?.split(" ")
      .map((n) => n[0])
      .join("")
      .toUpperCase()
      .slice(0, 2) ||
    user.email?.slice(0, 2).toUpperCase() ||
    "?";

  // Only the first name in the compact footer preview so it never truncates.
  const firstName = user.displayName?.trim().split(/\s+/)[0] || "User";

  return (
    <div className="flex items-center gap-2 px-1 py-1">
      {/* Compact identity preview — clicking it opens the same menu. Expanded only. */}
      <button
        type="button"
        aria-label="Open account menu"
        onClick={() => setMenuOpen(true)}
        className="hover:bg-accent flex min-w-0 flex-1 items-center gap-2 rounded-md p-1 text-left group-data-[collapsible=icon]:hidden"
      >
        <Avatar className="size-7 shrink-0">
          <AvatarImage
            src={user.photoUrl}
            alt={user.displayName || user.email}
          />
          <AvatarFallback className="text-xs">{userInitials}</AvatarFallback>
        </Avatar>
        <span className="truncate text-sm font-medium">{firstName}</span>
      </button>

      {/* Smaller inline theme switcher — expanded only */}
      <ThemeSwitcher className="scale-90 group-data-[collapsible=icon]:hidden" />

      <DropdownMenu open={menuOpen} onOpenChange={setMenuOpen}>
        <DropdownMenuTrigger asChild>
          <button
            data-testid="user-menu-trigger"
            type="button"
            aria-label="Account menu"
            className="border-border text-muted-foreground hover:bg-accent hover:text-foreground flex size-7 shrink-0 items-center justify-center rounded-full border group-data-[collapsible=icon]:mx-auto"
          >
            <MoreHorizontal className="h-4 w-4 group-data-[collapsible=icon]:hidden" />
            {/* Collapsed: the round trigger shows the avatar so the menu stays reachable */}
            <Avatar className="hidden size-7 group-data-[collapsible=icon]:block">
              <AvatarImage
                src={user.photoUrl}
                alt={user.displayName || user.email}
              />
              <AvatarFallback className="text-xs">
                {userInitials}
              </AvatarFallback>
            </Avatar>
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent side="top" align="end" className="w-56">
          <DropdownMenuLabel className="font-normal">
            <div className="flex items-start justify-between gap-2">
              <div className="flex flex-col space-y-1">
                <p className="text-sm leading-none font-medium">
                  {user.displayName || "User"}
                </p>
                <p className="text-muted-foreground text-xs leading-none">
                  {user.email}
                </p>
              </div>
              {projectSlug && (
                <button
                  type="button"
                  aria-label="Project Settings"
                  onClick={() => routes.settings.goTo()}
                  className="text-muted-foreground hover:text-foreground"
                >
                  <SettingsIcon className="h-4 w-4" />
                </button>
              )}
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
              <DropdownMenuItem onClick={() => orgRoutes.adminSettings.goTo()}>
                <ArrowRightLeftIcon className="mr-2 h-4 w-4" />
                Organization Override
              </DropdownMenuItem>
            )}
            {isMultiOrg && (
              <DropdownMenuItem onClick={() => navigate("/switch-org")}>
                <BuildingIcon className="mr-2 h-4 w-4" />
                Switch Organization
              </DropdownMenuItem>
            )}
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
          <DropdownMenuGroup>
            <DropdownMenuItem asChild>
              <a
                href="https://www.speakeasy.com/docs/mcp"
                target="_blank"
                rel="noopener noreferrer"
              >
                <BookOpenIcon className="mr-2 h-4 w-4" />
                Docs
              </a>
            </DropdownMenuItem>
            <DropdownMenuItem asChild>
              <a
                href="https://www.speakeasy.com/changelog?product=mcp-platform"
                target="_blank"
                rel="noopener noreferrer"
              >
                <PencilIcon className="mr-2 h-4 w-4" />
                Changelog
              </a>
            </DropdownMenuItem>
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
                href="https://roadmap.speakeasy.com"
                target="_blank"
                rel="noopener noreferrer"
              >
                <MapIcon className="mr-2 h-4 w-4" />
                Roadmap
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
  );
}
