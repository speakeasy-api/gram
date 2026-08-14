import type { JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useMatchRoute } from "@tanstack/react-router";
import {
  BuildingIcon,
  ChevronLeftIcon,
  FolderIcon,
  UsersIcon,
} from "lucide-react";

import {
  SidebarGroup,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarSeparator,
} from "@/components/ui/sidebar";
import { organizationProjectsQuery } from "@/lib/adminQueries";
import type { AdminOrganization } from "@/lib/gramAdminApi";
import { TRIAL_LABELS } from "@/lib/trialLabels";

// Indexed as a plain string record, for the reason `Trial` gives: the server
// can send a state this build has never heard of.
const WORDS_BY_STATE: Record<string, string | undefined> = TRIAL_LABELS;

// Account type, and the trial state in the one wording the row badge uses. A
// state with no words here is left out rather than read as "No trial": that
// would launder an unknown state into a known one.
function subtitle(org: AdminOrganization): string {
  const state = org.trial_state ?? "none";
  const words = Object.hasOwn(TRIAL_LABELS, state)
    ? WORDS_BY_STATE[state]
    : undefined;
  return words ? `${org.account_type} · ${words}` : org.account_type;
}

// `idOrSlug` is the address the operator is on, not `org.slug`. Rewriting it
// would move the record to another cache entry on the next nav press.
export function RecordNav({
  idOrSlug,
  org,
}: {
  idOrSlug: string;
  org: AdminOrganization;
}): JSX.Element {
  const matchRoute = useMatchRoute();
  const { data } = useQuery(organizationProjectsQuery(org.id));

  // Undefined until the query resolves. `?? 0` here would paint a zero over a
  // question nothing has answered yet.
  const projectCount = data?.projects.length;
  const onlyProject = projectCount === 1 ? data?.projects[0] : undefined;

  // Fuzzy, so the item stays lit while one of its projects is shown. Overview
  // is the record's index, so it is the exact match and nothing else.
  const onProjects = !!matchRoute({
    to: "/organizations/$idOrSlug/projects",
    params: { idOrSlug },
    fuzzy: true,
  });

  return (
    <>
      <SidebarGroup className="pb-0">
        <SidebarGroupContent>
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarMenuButton
                asChild
                className="text-muted-foreground"
                tooltip="All organizations"
              >
                <Link to="/organizations">
                  <ChevronLeftIcon />
                  <span>All organizations</span>
                </Link>
              </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarGroupContent>
      </SidebarGroup>

      <div className="flex items-center gap-2 px-4 py-2 group-data-[collapsible=icon]:hidden">
        <span
          aria-hidden="true"
          className="bg-sidebar-accent text-sidebar-accent-foreground flex size-6 shrink-0 items-center justify-center rounded-md text-xs font-medium"
        >
          {org.name.trim().slice(0, 1).toUpperCase()}
        </span>
        <div className="grid min-w-0">
          <span className="truncate text-sm font-medium">{org.name}</span>
          <span className="text-muted-foreground truncate text-xs">
            {subtitle(org)}
          </span>
        </div>
      </div>

      <SidebarSeparator className="mx-4 w-auto" />

      <SidebarGroup>
        <SidebarGroupContent>
          <SidebarMenu>
            <SidebarMenuItem>
              <SidebarMenuButton
                asChild
                isActive={
                  !!matchRoute({
                    to: "/organizations/$idOrSlug",
                    params: { idOrSlug },
                  })
                }
                tooltip="Overview"
              >
                <Link to="/organizations/$idOrSlug" params={{ idOrSlug }}>
                  <BuildingIcon />
                  <span>Overview</span>
                </Link>
              </SidebarMenuButton>
            </SidebarMenuItem>

            <SidebarMenuItem>
              <SidebarMenuButton
                asChild
                isActive={onProjects}
                tooltip="Projects"
              >
                {onlyProject ? (
                  <Link
                    to="/organizations/$idOrSlug/projects/$projectIdOrSlug"
                    params={{
                      idOrSlug,
                      projectIdOrSlug: onlyProject.slug || onlyProject.id,
                    }}
                  >
                    <FolderIcon />
                    <span>Projects</span>
                  </Link>
                ) : (
                  <Link
                    to="/organizations/$idOrSlug/projects"
                    params={{ idOrSlug }}
                  >
                    <FolderIcon />
                    <span>Projects</span>
                  </Link>
                )}
              </SidebarMenuButton>
              {projectCount !== undefined && projectCount > 1 && (
                <SidebarMenuBadge>{projectCount}</SidebarMenuBadge>
              )}
            </SidebarMenuItem>

            <SidebarMenuItem>
              <SidebarMenuButton
                asChild
                isActive={
                  !!matchRoute({
                    to: "/organizations/$idOrSlug/members",
                    params: { idOrSlug },
                  })
                }
                tooltip="Members"
              >
                <Link
                  to="/organizations/$idOrSlug/members"
                  params={{ idOrSlug }}
                >
                  <UsersIcon />
                  <span>Members</span>
                </Link>
              </SidebarMenuButton>
              {/* No query and no pending state: the count came with the
                  record. */}
              <SidebarMenuBadge>{org.member_count}</SidebarMenuBadge>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarGroupContent>
      </SidebarGroup>
    </>
  );
}
