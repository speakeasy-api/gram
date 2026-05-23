import { useCallback, useState } from "react";

import { useIsAdmin, useUser } from "@/contexts/Auth";
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
  BugIcon,
  CreditCardIcon,
  LogOutIcon,
  MailIcon,
  MessageCircleIcon,
  SettingsIcon,
} from "lucide-react";

import { Avatar, AvatarFallback, AvatarImage } from "./ui/avatar";

export function SidebarUserMenu() {
  const routes = useRoutes();
  const orgRoutes = useOrgRoutes();
  const user = useUser();
  const isAdmin = useIsAdmin();
  const client = useSdkClient();
  const { projectSlug } = useSlugs();
  const { hasAnyScope } = useRBAC();
  const canAccessOrgRoutes = hasAnyScope(["org:read", "org:admin"]);
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

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          className="hover:bg-sidebar-accent flex w-full items-center gap-2.5 rounded-md px-2 py-2 text-left transition-colors group-data-[collapsible=icon]:justify-center"
        >
          <Avatar className="size-7 shrink-0">
            <AvatarImage
              src={user.photoUrl}
              alt={user.displayName || user.email}
            />
            <AvatarFallback className="text-[10px]">
              {userInitials}
            </AvatarFallback>
          </Avatar>
          <div className="min-w-0 flex-1 group-data-[collapsible=icon]:hidden">
            <div className="text-foreground truncate text-sm font-medium">
              {user.displayName || "User"}
            </div>
            <div className="text-muted-foreground truncate text-xs">
              {user.email}
            </div>
          </div>
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        side="top"
        className="w-56"
        sideOffset={8}
      >
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
            <DropdownMenuItem onClick={() => orgRoutes.adminSettings.goTo()}>
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
        <div className="flex items-center justify-between px-2 py-1.5">
          <span className="text-muted-foreground text-xs">Theme</span>
          <ThemeSwitcher />
        </div>
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
  );
}
