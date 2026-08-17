import type { AriaAttributes, JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useMatchRoute } from "@tanstack/react-router";
import {
  BuildingIcon,
  ChevronLeftIcon,
  ExternalLinkIcon,
  FolderIcon,
  SlidersHorizontalIcon,
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
import { LEAVES_THE_APP, organizationFeaturesUrl } from "@/lib/impersonation";
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

// What the nav computed, in the words a screen reader is told. Held apart from
// `currentProps` for the one item that is not a `Link` and so has no guess to
// turn off.
function ariaCurrent(isCurrent: boolean): AriaAttributes["aria-current"] {
  return isCurrent ? "page" : undefined;
}

// Both answers about one item, from the one boolean the nav computes. Without
// `exact`, `Link` marks itself aria-current="page" whenever the current address
// merely starts with its target, and every record address starts with
// `/organizations` and with the record's own index, so three items claim the
// page at once. `Link` also writes that guess over an aria-current handed to it,
// so turning the guess off is what lets the value below be the answer.
// site-header.tsx:53 names the same default.
function currentProps(isCurrent: boolean): {
  activeOptions: { exact: boolean };
  "aria-current": AriaAttributes["aria-current"];
} {
  return {
    activeOptions: { exact: true },
    "aria-current": ariaCurrent(isCurrent),
  };
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

  // The one item built from `org.slug` rather than the address: the server
  // reads the organization back out of the redirect's first segment, so an id
  // there lands the operator nowhere.
  const featuresUrl = organizationFeaturesUrl(org.slug);

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
  const onOverview = !!matchRoute({
    to: "/organizations/$idOrSlug",
    params: { idOrSlug },
  });
  const onMembers = !!matchRoute({
    to: "/organizations/$idOrSlug/members",
    params: { idOrSlug },
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
                <Link to="/organizations" {...currentProps(false)}>
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
                isActive={onOverview}
                tooltip="Overview"
              >
                <Link
                  to="/organizations/$idOrSlug"
                  params={{ idOrSlug }}
                  {...currentProps(onOverview)}
                >
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
                    {...currentProps(onProjects)}
                  >
                    <FolderIcon />
                    <span>Projects</span>
                  </Link>
                ) : (
                  <Link
                    to="/organizations/$idOrSlug/projects"
                    params={{ idOrSlug }}
                    {...currentProps(onProjects)}
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

            {/* Absent rather than dead when the record has no slug or no app
                origin is configured, the way the header's Open in Gram is. */}
            {featuresUrl && (
              <SidebarMenuItem>
                <SidebarMenuButton asChild tooltip="Features">
                  {/* A plain anchor, not a `Link`: this is the only item that
                      leaves the admin app, because every admin endpoint for
                      the feature switches resolves the operator's own
                      organization rather than this record. AGE-3242. It is not
                      an address here, so it can never be the current page. */}
                  <a
                    href={featuresUrl}
                    target="_blank"
                    rel="noreferrer"
                    aria-current={ariaCurrent(false)}
                  >
                    <SlidersHorizontalIcon />
                    <span>
                      Features
                      <span className="sr-only">{LEAVES_THE_APP}</span>
                    </span>
                    <ExternalLinkIcon
                      aria-hidden="true"
                      className="ml-auto opacity-60 group-data-[collapsible=icon]:hidden"
                    />
                  </a>
                </SidebarMenuButton>
              </SidebarMenuItem>
            )}

            <SidebarMenuItem>
              <SidebarMenuButton asChild isActive={onMembers} tooltip="Members">
                <Link
                  to="/organizations/$idOrSlug/members"
                  params={{ idOrSlug }}
                  {...currentProps(onMembers)}
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
