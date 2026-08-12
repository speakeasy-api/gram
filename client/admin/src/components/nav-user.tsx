import type { JSX } from "react";
import {
  LogOutIcon,
  MoreVerticalIcon,
  RefreshCwIcon,
  TriangleAlertIcon,
} from "lucide-react";
import { useMutation, useQuery } from "@tanstack/react-query";

import { adminSessionQuery, logout } from "@/lib/gramAdminApi";
import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from "@/components/ui/sidebar";

// Takes the first letter of the first two words, for example "Ada Lovelace"
// becomes "AL".
function initials(name: string): string {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((word) => word.slice(0, 1).toUpperCase())
    .join("");
}

export function NavUser(): JSX.Element {
  const { isMobile } = useSidebar();

  const session = useQuery(adminSessionQuery);

  const logoutMutation = useMutation({ mutationFn: logout });

  // AuthGate settles the session before the router mounts, so there is no
  // loading state to render here. A failed background refetch keeps the last
  // session, so the menu only gives up when it holds nothing to render.
  if (!session.data) {
    return (
      <SidebarMenu>
        <SidebarMenuItem>
          <SidebarMenuButton
            tooltip="Session unavailable. Select to retry."
            disabled={session.isFetching}
            onClick={() => void session.refetch()}
          >
            <TriangleAlertIcon className="text-destructive" />
            <span className="truncate">Session unavailable</span>
            <RefreshCwIcon className="ml-auto" />
          </SidebarMenuButton>
        </SidebarMenuItem>
      </SidebarMenu>
    );
  }

  const { email, name } = session.data;
  const label = name || email;

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <SidebarMenuButton
              size="lg"
              className="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
            >
              <Avatar className="rounded-lg grayscale">
                <AvatarFallback className="rounded-lg">
                  {initials(label)}
                </AvatarFallback>
              </Avatar>
              <div className="grid flex-1 text-left text-sm leading-tight">
                <span className="truncate font-medium">{label}</span>
                <span className="text-muted-foreground truncate text-xs">
                  {email}
                </span>
              </div>
              <MoreVerticalIcon className="ml-auto size-4" />
            </SidebarMenuButton>
          </DropdownMenuTrigger>

          <DropdownMenuContent
            className="w-(--radix-dropdown-menu-trigger-width) min-w-56 rounded-lg"
            side={isMobile ? "bottom" : "right"}
            align="end"
            sideOffset={4}
          >
            <DropdownMenuLabel className="p-0 font-normal">
              <div className="flex items-center gap-2 px-1 py-1.5 text-left text-sm">
                <Avatar className="rounded-lg">
                  <AvatarFallback className="rounded-lg">
                    {initials(label)}
                  </AvatarFallback>
                </Avatar>
                <div className="grid flex-1 text-left text-sm leading-tight">
                  <span className="truncate font-medium">{label}</span>
                  <span className="text-muted-foreground truncate text-xs">
                    {email}
                  </span>
                </div>
              </div>
            </DropdownMenuLabel>

            <DropdownMenuSeparator />

            <DropdownMenuItem
              disabled={logoutMutation.isPending}
              // The item closes the menu on select, which unmounts the error
              // below before anyone reads it. Keep the menu open instead.
              onSelect={(event) => {
                event.preventDefault();
                logoutMutation.mutate();
              }}
            >
              <LogOutIcon />
              Log out
            </DropdownMenuItem>

            {logoutMutation.isError && (
              <p className="text-destructive px-2 py-1.5 text-xs">
                Log out failed. Try again.
              </p>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}
