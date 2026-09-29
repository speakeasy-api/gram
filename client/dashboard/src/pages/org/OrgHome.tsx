import { InputDialog } from "@/components/input-dialog";
import { Page } from "@/components/page-layout";
import { MemberFacepile } from "@/components/member-facepile";
import { ProjectAvatar } from "@/components/project-menu";
import { DEFAULT_DATE_RANGE_PRESET } from "@/components/observe/useDateRangeFilter";
import { buildProjectOverviewQuery } from "@/components/project/projectOverviewQuery";
import { RequireScope } from "@/components/require-scope";
import { CardContextMenu } from "@/components/card-context-menu";
import { TableRowContextMenu } from "@/components/table-row-context-menu";
import { Button } from "@/components/ui/Button";
import type { Action } from "@/components/ui/MoreActions";
import { SearchBar } from "@/components/ui/SearchBar";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/Tooltip";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { useLocalStorageState } from "@/hooks/useLocalStorageState";
import { useProjectFavorites } from "@/hooks/useProjectFavorites";
import { useRBAC } from "@/hooks/useRBAC";
import { dateTimeFormatters } from "@/lib/dates";
import { getPreferredProject } from "@/lib/preferredProject";
import { cn } from "@/lib/utils";
import type { AccessMember } from "@gram/client/models/components/accessmember.js";
import type { AuditLog } from "@gram/client/models/components/auditlog.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useAuditLogs } from "@gram/client/react-query/auditLogs.js";
import { useMembers } from "@gram/client/react-query/members.js";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { useQueryClient } from "@tanstack/react-query";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { type IconName } from "@/components/ui/Icon/names";
import {
  ChevronDown,
  ChevronUp,
  Copy,
  History,
  LayoutGrid,
  List,
  MoreHorizontal,
  Plus,
  Settings,
  Star,
  type LucideIcon,
} from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate } from "react-router";

import { getActorLabel, renderVerb } from "@/lib/audit-log-format";

const PROJECT_LIMIT = 6;
const FACEPILE_LIMIT = 10;

type OrgProject = ReturnType<typeof useOrganization>["projects"][number];

export default function OrgHome(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope
          scope={["org:read", "project:read", "org:admin"]}
          level="page"
        >
          <OrgHomeInner />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

function OrgHomeInner() {
  const organization = useOrganization();
  const { orgSlug } = useSlugs();
  const client = useSdkClient();
  const navigate = useNavigate();
  const { hasScope } = useRBAC();
  const canAdmin = hasScope("org:admin");

  const [search, setSearch] = useState("");
  const [expanded, setExpanded] = useState(false);
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [newProjectName, setNewProjectName] = useState("");
  const [viewMode, setViewMode] = useLocalStorageState<"list" | "grid">(
    "gram:org-home-view",
    "list",
  );

  const { favoriteSet, isFavorite, toggleFavorite } = useProjectFavorites(
    organization.id,
  );

  // Warm the overview cache for the one project the user is most likely to
  // open next (last visited, else `default`). Same feature gate as
  // ProjectDashboard; staleTime dedupes re-runs and the fetch on navigation.
  const gramClient = useGramContext();
  const queryClient = useQueryClient();
  const { data: featuresData } = useProductFeatures({
    organizationId: organization.id,
  });
  const logsEnabled = featuresData?.logsEnabled === true;
  const prefetchProject =
    getPreferredProject(organization.projects) ??
    organization.projects.find((p) => p.slug === "default") ??
    organization.projects[0];
  const prefetchProjectSlug = prefetchProject?.slug;
  const organizationSlug = organization.slug;

  useEffect(() => {
    if (!logsEnabled || !prefetchProjectSlug || !organizationSlug) return;
    void queryClient.prefetchQuery(
      buildProjectOverviewQuery(gramClient, {
        organization: organizationSlug,
        project: prefetchProjectSlug,
        range: { preset: DEFAULT_DATE_RANGE_PRESET },
      }),
    );
  }, [
    logsEnabled,
    prefetchProjectSlug,
    organizationSlug,
    gramClient,
    queryClient,
  ]);

  // Fetch org-wide audit log once. We use it to drive (a) the left rail
  // preview, (b) each project's "most recent action", and (c) the facepile
  // of active actors per project — all from one network call.
  const { data: auditData } = useAuditLogs();
  const auditLogs = useMemo(() => auditData?.result.logs ?? [], [auditData]);

  const { data: membersData } = useMembers();
  const memberById = useMemo(() => {
    const map = new Map<string, AccessMember>();
    for (const m of membersData?.members ?? []) map.set(m.id, m);
    return map;
  }, [membersData]);

  const { latestActionByProjectSlug, activeActorsByProjectSlug } =
    useMemo(() => {
      const latest = new Map<string, AuditLog>();
      const actors = new Map<string, string[]>();
      for (const log of auditLogs) {
        if (!log.projectSlug) continue;
        if (!latest.has(log.projectSlug)) latest.set(log.projectSlug, log);
        if (log.actorType !== "user") continue;
        const list = actors.get(log.projectSlug) ?? [];
        // Preserve recency order; dedupe.
        if (!list.includes(log.actorId)) {
          list.push(log.actorId);
          actors.set(log.projectSlug, list);
        }
      }
      return {
        latestActionByProjectSlug: latest,
        activeActorsByProjectSlug: actors,
      };
    }, [auditLogs]);

  // Fallback facepile when a project has no audit activity yet — show a
  // stable, deterministic slice of org members so a fresh project still feels
  // populated. Sorted by joinedAt so the choice is reproducible across loads.
  const fallbackMembers = useMemo(() => {
    return [...(membersData?.members ?? [])]
      .sort((a, b) => a.joinedAt.getTime() - b.joinedAt.getTime())
      .slice(0, FACEPILE_LIMIT);
  }, [membersData]);

  const filteredProjects = useMemo(
    () =>
      [...organization.projects]
        .filter((project) => {
          if (!search) return true;
          const query = search.toLowerCase();
          return (
            project.name.toLowerCase().includes(query) ||
            project.slug.toLowerCase().includes(query)
          );
        })
        .sort((a, b) => a.name.localeCompare(b.name)),
    [organization.projects, search],
  );

  const isSearching = search.length > 0;

  const { favoriteProjects, otherProjects } = useMemo(() => {
    if (isSearching) {
      return { favoriteProjects: [], otherProjects: filteredProjects };
    }
    const favs: OrgProject[] = [];
    const rest: OrgProject[] = [];
    for (const p of filteredProjects) {
      if (favoriteSet.has(p.id)) favs.push(p);
      else rest.push(p);
    }
    return { favoriteProjects: favs, otherProjects: rest };
  }, [filteredProjects, favoriteSet, isSearching]);

  const hasMore = !isSearching && otherProjects.length > PROJECT_LIMIT;
  const visibleOtherProjects =
    expanded || isSearching
      ? otherProjects
      : otherProjects.slice(0, PROJECT_LIMIT);

  const createProject = async (name: string) => {
    const result = await client.projects.create({
      createProjectRequestBody: {
        name,
        organizationId: organization.id,
      },
    });
    setNewProjectName("");
    void navigate(`/${orgSlug}/projects/${result.project.slug}`);
  };

  const getFacepileMembers = (projectSlug: string): AccessMember[] => {
    const actorIds = activeActorsByProjectSlug.get(projectSlug) ?? [];
    const resolved: AccessMember[] = [];
    for (const id of actorIds) {
      const m = memberById.get(id);
      if (m) resolved.push(m);
      if (resolved.length >= FACEPILE_LIMIT) break;
    }
    if (resolved.length > 0) return resolved;
    return fallbackMembers;
  };

  const renderProjectItem = (project: OrgProject) => {
    const props = {
      project,
      latestLog: latestActionByProjectSlug.get(project.slug),
      facepile: getFacepileMembers(project.slug),
      isFavorite: isFavorite(project.id),
      onToggleFavorite: () => toggleFavorite(project.id),
    };
    return viewMode === "grid" ? (
      <ProjectCard key={project.id} {...props} />
    ) : (
      <ProjectRow key={project.id} {...props} />
    );
  };

  const renderProjectContainer = (children: React.ReactNode) =>
    viewMode === "grid" ? (
      <ProjectGrid>{children}</ProjectGrid>
    ) : (
      <ProjectList>{children}</ProjectList>
    );

  return (
    <>
      <Page.Section>
        <Page.Section.Title>Projects</Page.Section.Title>
        <Page.Section.Description>
          Create a project for each team or environment that needs its own MCP
          servers, skills, plugins and access, such as separate staging and
          production, or a team whose tools others should not see.
        </Page.Section.Description>
        <Page.Section.Body>
          <div className="flex min-w-0 flex-col gap-6">
            <div className="flex items-center gap-2">
              <SearchBar
                value={search}
                onChange={setSearch}
                placeholder="Search projects..."
                className="flex-1"
              />
              <ViewModeToggle value={viewMode} onChange={setViewMode} />
              {canAdmin && (
                <Button onClick={() => setCreateDialogOpen(true)}>
                  <Plus className="size-4" />
                  New project
                </Button>
              )}
            </div>

            {/* The page title already says Projects, so no titled card:
                list rows sit in one bordered panel, grid cards stand alone
                rather than as boxes inside a box. */}
            <div
              className={cn(
                viewMode === "list" && "border-border bg-card border",
              )}
            >
              {filteredProjects.length === 0 && isSearching ? (
                <div className="border-border bg-card flex flex-col items-center gap-3 border border-dashed py-12 text-center">
                  <Text muted>No projects matching &ldquo;{search}&rdquo;</Text>
                  <RequireScope scope="org:admin" level="component">
                    <Button
                      size="sm"
                      onClick={() => {
                        setNewProjectName(search);
                        setCreateDialogOpen(true);
                      }}
                    >
                      <Plus className="size-4" />
                      Create &ldquo;{search}&rdquo;
                    </Button>
                  </RequireScope>
                </div>
              ) : (
                <>
                  {favoriteProjects.length > 0 && (
                    <>
                      <section className="flex flex-col">
                        <SectionDivider
                          label="Favorites"
                          inset={viewMode === "list"}
                        />
                        {renderProjectContainer(
                          favoriteProjects.map(renderProjectItem),
                        )}
                      </section>
                      {visibleOtherProjects.length > 0 && (
                        <SectionDivider
                          label="All projects"
                          inset={viewMode === "list"}
                        />
                      )}
                    </>
                  )}

                  {renderProjectContainer(
                    visibleOtherProjects.map(renderProjectItem),
                  )}
                  {hasMore && (
                    <button
                      type="button"
                      onClick={() => setExpanded((prev) => !prev)}
                      className={cn(
                        "text-muted-foreground hover:text-foreground border-border hover:bg-muted/40 flex w-full items-center justify-center gap-1.5 border-dashed py-3 text-sm font-medium transition-colors",
                        viewMode === "list" ? "border-t" : "mt-4 border",
                      )}
                    >
                      {expanded ? (
                        <>
                          Show less
                          <ChevronUp className="size-4" />
                        </>
                      ) : (
                        <>
                          Show all {otherProjects.length} projects
                          <ChevronDown className="size-4" />
                        </>
                      )}
                    </button>
                  )}

                  {otherProjects.length === 0 &&
                    favoriteProjects.length === 0 && (
                      <div className="border-border bg-card flex flex-col items-center gap-3 border border-dashed py-12 text-center">
                        <Text muted>No projects yet</Text>
                        <RequireScope scope="org:admin" level="component">
                          <Button
                            size="sm"
                            onClick={() => setCreateDialogOpen(true)}
                          >
                            <Plus className="size-4" />
                            Create your first project
                          </Button>
                        </RequireScope>
                      </div>
                    )}
                </>
              )}
            </div>
          </div>
        </Page.Section.Body>
      </Page.Section>

      {createDialogOpen && (
        <InputDialog
          open={createDialogOpen}
          onOpenChange={setCreateDialogOpen}
          title="Create New Project"
          description="Create a new project to organize your MCP servers, tools, and integrations."
          submitButtonText="Create Project"
          onSubmit={() => createProject(newProjectName)}
          inputs={[
            {
              label: "Name",
              value: newProjectName,
              onChange: setNewProjectName,
              placeholder: "My Project",
            },
          ]}
        />
      )}
    </>
  );
}

function ProjectList({ children }: { children: React.ReactNode }) {
  // No border of its own — the list panel around it draws one, and nesting
  // two reads as a box in a box.
  return (
    <div className="border-border divide-border divide-y overflow-hidden">
      {children}
    </div>
  );
}

/**
 * Short stub, label, then a rule out to the card edge — separates the project
 * groups so favorites and "All projects" read as the same kind of break.
 * `inset` lines the stub up with the row padding in list mode, where the rows
 * themselves run edge to edge.
 */
function SectionDivider({ label, inset }: { label: string; inset: boolean }) {
  return (
    <div className={cn("flex items-center gap-3 pt-5 pb-2", inset && "px-4")}>
      <div aria-hidden="true" className="bg-border h-px w-6 shrink-0" />
      {/* A real heading, not decoration: it is the only thing distinguishing
          the favourites group from the rest of the list for screen readers. */}
      <Text
        as="h3"
        muted
        small
        className="text-muted-foreground/80 shrink-0 font-normal"
      >
        {label}
      </Text>
      <div aria-hidden="true" className="bg-border h-px flex-1" />
    </div>
  );
}

function ProjectGrid({ children }: { children: React.ReactNode }) {
  return (
    <div className="grid grid-cols-1 gap-6 sm:grid-cols-2 xl:grid-cols-3">
      {children}
    </div>
  );
}

function ViewModeToggle({
  value,
  onChange,
}: {
  value: "list" | "grid";
  onChange: (mode: "list" | "grid") => void;
}) {
  return (
    <div className="border-border bg-card flex h-[42px] shrink-0 items-center gap-0.5 border p-1">
      <ViewModeButton
        active={value === "grid"}
        onClick={() => onChange("grid")}
        ariaLabel="Grid view"
      >
        <LayoutGrid className="size-4" strokeWidth={1.75} />
      </ViewModeButton>
      <ViewModeButton
        active={value === "list"}
        onClick={() => onChange("list")}
        ariaLabel="List view"
      >
        <List className="size-4" strokeWidth={1.75} />
      </ViewModeButton>
    </div>
  );
}

function ViewModeButton({
  active,
  onClick,
  ariaLabel,
  children,
}: {
  active: boolean;
  onClick: () => void;
  ariaLabel: string;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={ariaLabel}
      aria-pressed={active}
      className={cn(
        "flex size-8 items-center justify-center transition-colors",
        active
          ? "bg-muted text-foreground"
          : "text-muted-foreground hover:bg-muted/60 hover:text-foreground",
      )}
    >
      {children}
    </button>
  );
}

function ProjectRow({
  project,
  latestLog,
  facepile,
  isFavorite,
  onToggleFavorite,
}: {
  project: OrgProject;
  latestLog: AuditLog | undefined;
  facepile: AccessMember[];
  isFavorite: boolean;
  onToggleFavorite: () => void;
}) {
  const { orgSlug } = useSlugs();
  const actions = useProjectActions(project, { isFavorite, onToggleFavorite });

  return (
    <TableRowContextMenu actions={actions}>
      {/* Below `md` the row stacks: identity and summary first, then the
          facepile and actions on their own line — side by side there is not
          enough width for the summary without it running under the faces. */}
      <div
        className={cn(
          "group hover:bg-muted/40 relative flex flex-col gap-3 px-4 py-4 transition-colors md:flex-row md:items-center md:gap-4",
          isFavorite && "bg-muted/30",
        )}
      >
        <div className="flex min-w-0 flex-1 items-center gap-4">
          {/* Decorative content: pointer-events-none routes clicks through to
              the Link overlay below, while the actions region opts back in. */}
          <ProjectAvatar
            project={project}
            className="pointer-events-none h-9 w-9 shrink-0"
          />

          <div className="pointer-events-none flex min-w-0 flex-1 flex-col gap-1 sm:flex-row sm:items-center sm:gap-6">
            <div className="min-w-0 sm:w-44 sm:shrink-0">
              <Text
                variant="subheading"
                as="div"
                className="text-foreground truncate text-sm font-medium"
              >
                {project.name}
              </Text>
              <Text small muted className="truncate font-mono text-xs">
                {project.slug}
              </Text>
            </div>

            <div className="min-w-0 flex-1">
              <RecentActionBlock log={latestLog} />
            </div>
          </div>
        </div>

        <div className="flex items-center justify-between gap-3 pl-13 md:justify-end md:pl-0">
          <div
            className="relative z-10 flex"
            onClick={(e) => {
              // Keep clicks on the facepile from triggering the row's Link overlay.
              e.preventDefault();
              e.stopPropagation();
            }}
          >
            <MemberFacepile members={facepile} maxFaces={5} />
          </div>

          <ProjectRowActions
            actions={actions}
            isFavorite={isFavorite}
            onToggleFavorite={onToggleFavorite}
          />
        </div>

        {/* Anchor overlay sits on top of pointer-events-none children, so the
            entire row is one navigation target — interactive controls above
            opt in via pointer-events-auto. */}
        <Link
          to={`/${orgSlug}/projects/${project.slug}`}
          aria-label={`Open ${project.name}`}
          className="absolute inset-0"
        />
      </div>
    </TableRowContextMenu>
  );
}

function ProjectCard({
  project,
  latestLog,
  facepile,
  isFavorite,
  onToggleFavorite,
}: {
  project: OrgProject;
  latestLog: AuditLog | undefined;
  facepile: AccessMember[];
  isFavorite: boolean;
  onToggleFavorite: () => void;
}) {
  const { orgSlug } = useSlugs();
  const actions = useProjectActions(project, { isFavorite, onToggleFavorite });

  return (
    <CardContextMenu actions={actions}>
      {/* The card div keeps `relative`, so the Link overlay below still fills
          the card rather than the context-menu wrapper. */}
      <div
        className={cn(
          "group border-border bg-card hover:border-foreground relative flex h-full flex-col gap-3 border p-4 transition-colors",
          isFavorite && "bg-muted/30",
        )}
      >
        <div className="pointer-events-none flex items-start gap-3">
          <ProjectAvatar project={project} className="h-10 w-10 shrink-0" />
          <div className="min-w-0 flex-1">
            <Text
              variant="subheading"
              as="div"
              className="text-foreground truncate text-sm font-medium"
            >
              {project.name}
            </Text>
            <Text small muted className="truncate font-mono text-xs">
              {project.slug}
            </Text>
          </div>
        </div>

        <div className="pointer-events-none min-w-0 flex-1">
          <RecentActionBlock log={latestLog} />
        </div>

        <div className="flex items-center justify-between gap-2">
          <div
            className="relative z-10 min-w-0"
            onClick={(e) => {
              e.preventDefault();
              e.stopPropagation();
            }}
          >
            <MemberFacepile members={facepile} maxFaces={3} />
          </div>
          <ProjectRowActions
            actions={actions}
            isFavorite={isFavorite}
            onToggleFavorite={onToggleFavorite}
          />
        </div>

        <Link
          to={`/${orgSlug}/projects/${project.slug}`}
          aria-label={`Open ${project.name}`}
          className="absolute inset-0"
        />
      </div>
    </CardContextMenu>
  );
}

/**
 * The per-project actions shared by the visible "⋯" dropdown and the
 * right-click context menu, so both stay in sync.
 */
function useProjectActions(
  project: OrgProject,
  {
    isFavorite,
    onToggleFavorite,
  }: { isFavorite: boolean; onToggleFavorite: () => void },
): Action[] {
  const { orgSlug } = useSlugs();
  const navigate = useNavigate();

  return [
    {
      icon: "star",
      label: isFavorite ? "Remove from favorites" : "Add to favorites",
      onClick: onToggleFavorite,
    },
    {
      icon: "settings",
      label: "Project settings",
      onClick: () => {
        void navigate(`/${orgSlug}/projects/${project.slug}/settings`);
      },
    },
    {
      icon: "history",
      label: "View audit logs",
      onClick: () => {
        void navigate(`/${orgSlug}/audit-logs?project=${project.slug}`);
      },
    },
    {
      icon: "copy",
      label: "Copy slug",
      onClick: () => {
        void navigator.clipboard?.writeText(project.slug);
      },
    },
  ];
}

// Lucide equivalents of the moonshine icon names used by useProjectActions,
// so the dropdown keeps its existing lucide icons.
const projectActionIcons: Partial<Record<IconName, LucideIcon>> = {
  star: Star,
  settings: Settings,
  history: History,
  copy: Copy,
};

function ProjectRowActions({
  actions,
  isFavorite,
  onToggleFavorite,
}: {
  actions: Action[];
  isFavorite: boolean;
  onToggleFavorite: () => void;
}) {
  const [menuOpen, setMenuOpen] = useState(false);

  const closeAnd = (cb: () => void) => () => {
    setMenuOpen(false);
    cb();
  };

  return (
    <div
      className="relative z-10 flex shrink-0 items-center gap-1"
      onClick={(e) => {
        // Stop the absolute <Link> overlay from receiving clicks inside this region.
        e.preventDefault();
        e.stopPropagation();
      }}
    >
      <button
        type="button"
        onClick={(e) => {
          e.preventDefault();
          e.stopPropagation();
          onToggleFavorite();
        }}
        aria-label={isFavorite ? "Remove from favorites" : "Add to favorites"}
        aria-pressed={isFavorite}
        className={cn(
          "hover:bg-muted flex size-8 items-center justify-center transition-colors",
          isFavorite ? "text-foreground" : "text-muted-foreground",
        )}
      >
        <Star
          className={cn("size-4", isFavorite && "fill-current")}
          strokeWidth={1.5}
        />
      </button>
      <DropdownMenu open={menuOpen} onOpenChange={setMenuOpen}>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            aria-label="More actions"
            className="text-muted-foreground hover:bg-muted hover:text-foreground flex size-8 items-center justify-center transition-colors"
          >
            <MoreHorizontal className="size-4" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-48">
          {actions.map((action, index) => {
            const ActionIcon = action.icon
              ? projectActionIcons[action.icon]
              : undefined;
            return (
              <DropdownMenuItem
                key={index}
                disabled={action.disabled}
                onClick={closeAnd(action.onClick)}
              >
                {ActionIcon && (
                  <ActionIcon
                    className={cn(
                      "size-4",
                      action.icon === "star" && isFavorite && "fill-current",
                    )}
                  />
                )}
                {action.label}
              </DropdownMenuItem>
            );
          })}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

function RecentActionBlock({ log }: { log: AuditLog | undefined }) {
  if (!log) {
    return (
      <Text small muted className="text-xs">
        No recent activity
      </Text>
    );
  }
  const actor = getActorLabel(log);
  const verb = renderVerb(log);

  return (
    <div className="relative z-10 flex min-w-0 flex-col gap-0.5">
      <Text
        small
        className="text-foreground truncate text-sm leading-snug font-medium"
      >
        {verb}
      </Text>
      <Tooltip>
        <TooltipTrigger asChild>
          <span className="text-muted-foreground inline-flex max-w-full min-w-0 cursor-default text-xs">
            <span className="truncate">
              {dateTimeFormatters.humanize(log.createdAt, {
                includeTime: false,
              })}
              <span className="mx-1 opacity-60">·</span>
              {actor}
            </span>
          </span>
        </TooltipTrigger>
        <TooltipContent className="font-mono text-[11px]">
          <TimestampDetail date={log.createdAt} />
        </TooltipContent>
      </Tooltip>
    </div>
  );
}

function TimestampDetail({ date }: { date: Date }) {
  const utc = date.toLocaleString("en-US", {
    timeZone: "UTC",
    year: "numeric",
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
  const local = date.toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  });
  const tzAbbr =
    new Intl.DateTimeFormat(undefined, {
      timeZoneName: "short",
    })
      .formatToParts(date)
      .find((p) => p.type === "timeZoneName")?.value ?? "Local";
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-center gap-2">
        <span className="bg-background/20 px-1 py-0.5 text-[10px] uppercase">
          UTC
        </span>
        <span>{utc}</span>
      </div>
      <div className="flex items-center gap-2">
        <span className="bg-background/20 px-1 py-0.5 text-[10px] uppercase">
          {tzAbbr}
        </span>
        <span>{local}</span>
      </div>
    </div>
  );
}
