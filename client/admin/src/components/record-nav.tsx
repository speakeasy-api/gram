import type { AriaAttributes, JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useMatchRoute } from "@tanstack/react-router";
import {
  BuildingIcon,
  ChevronLeftIcon,
  CreditCardIcon,
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
import { TRIAL_LABELS } from "@/lib/trialLabels";

// Indexed as a plain string record, for the reason `Trial` gives: the server
// can send a state this build has never heard of.
const WORDS_BY_STATE: Record<string, string | undefined> = TRIAL_LABELS;

// Account type, and the trial state in the one wording the row badge uses. A
// state with no words here is left out rather than read as "No trial": that
// would launder an unknown state into a known one.
function subtitle(org: AdminOrganization): string {
  const state = org.trial_state ?? "none";
  // `none` excluded on its own, because it has words and the guard beside it
  // only drops a state that has none. The header draws no mark for `none` and
  // the list cell draws a dash.
  const words =
    state !== "none" && Object.hasOwn(TRIAL_LABELS, state)
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
  const onBilling = !!matchRoute({
    to: "/organizations/$idOrSlug/billing",
    params: { idOrSlug },
  });
  const onMembers = !!matchRoute({
    to: "/organizations/$idOrSlug/members",
    params: { idOrSlug },
  });
  const onFeatures = !!matchRoute({
    to: "/organizations/$idOrSlug/features",
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
                  // Always the id. Project slugs are unique only within an
                  // organization, so project.get resolves a slug across all of
                  // them, and "default" matches one project in every one.
                  <Link
                    to="/organizations/$idOrSlug/projects/$projectIdOrSlug"
                    params={{
                      idOrSlug,
                      projectIdOrSlug: onlyProject.id,
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
              {/* Dropped at exactly one, and only there. That is the case the
                  item's target already answers, so a "1" beside a link that
                  opens the project itself counts a list the operator will never
                  see. Zero is a fact about the record and keeps its badge. */}
              {projectCount !== undefined && projectCount !== 1 && (
                <SidebarMenuBadge>{projectCount}</SidebarMenuBadge>
              )}
            </SidebarMenuItem>

            <SidebarMenuItem>
              <SidebarMenuButton asChild isActive={onBilling} tooltip="Billing">
                <Link
                  to="/organizations/$idOrSlug/billing"
                  params={{ idOrSlug }}
                  {...currentProps(onBilling)}
                >
                  <CreditCardIcon />
                  <span>Billing</span>
                </Link>
              </SidebarMenuButton>
            </SidebarMenuItem>

            <SidebarMenuItem>
              <SidebarMenuButton asChild isActive={onFeatures} tooltip="Features">
                <Link
                  to="/organizations/$idOrSlug/features"
                  params={{ idOrSlug }}
                  {...currentProps(onFeatures)}
                >
                  <SlidersHorizontalIcon />
                  <span>Features</span>
                </Link>
              </SidebarMenuButton>
            </SidebarMenuItem>

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
