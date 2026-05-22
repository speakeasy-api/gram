import { InputDialog } from "@/components/input-dialog";
import { Page } from "@/components/page-layout";
import { ProjectAvatar } from "@/components/project-menu";
import { RequireScope } from "@/components/require-scope";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { Heading } from "@/components/ui/heading";
import { SearchBar } from "@/components/ui/search-bar";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useProjectFavorites } from "@/hooks/useProjectFavorites";
import { useRBAC } from "@/hooks/useRBAC";
import { dateTimeFormatters } from "@/lib/dates";
import { cn } from "@/lib/utils";
import { ChallengesEmptyState } from "@/pages/access/ChallengesTab";
import {
  getInitials,
  isDisplayableBucket,
} from "@/pages/access/challengeHelpers";
import { useChallengeRowColumns } from "@/pages/access/useChallengeRowColumns";
import { useGrantFlow } from "@/pages/access/useGrantFlow";
import { useOrgRoutes } from "@/routes";
import type { AccessMember, AuditLog } from "@gram/client/models/components";
import { Outcome } from "@gram/client/models/operations/listchallengebuckets.js";
import { useAuditLogs } from "@gram/client/react-query";
import { useChallengeBuckets } from "@gram/client/react-query/challengeBuckets.js";
import { useMembers } from "@gram/client/react-query/members.js";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Table,
} from "@speakeasy-api/moonshine";
import {
  ChevronDown,
  ChevronUp,
  Copy,
  History,
  MoreHorizontal,
  Plus,
  Settings,
  ShieldCheck,
  Star,
  UserPlus,
} from "lucide-react";
import { useMemo, useState } from "react";
import { Link, useNavigate } from "react-router";

import { getActorLabel, renderVerb } from "./OrgAuditLogs";

const PROJECT_LIMIT = 6;
const AUDIT_PREVIEW_LIMIT = 8;
const CHALLENGE_PREVIEW_LIMIT = 3;
const FACEPILE_LIMIT = 10;

type OrgProject = ReturnType<typeof useOrganization>["projects"][number];

export default function OrgHome() {
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

export function OrgHomeInner() {
  const organization = useOrganization();
  const { orgSlug } = useSlugs();
  const client = useSdkClient();
  const navigate = useNavigate();
  const telemetry = useTelemetry();
  const { hasScope } = useRBAC();
  const isRbacEnabled = telemetry.isFeatureEnabled("gram-rbac") ?? false;
  const canAdmin = hasScope("org:admin");
  const orgRoutes = useOrgRoutes();

  const [search, setSearch] = useState("");
  const [expanded, setExpanded] = useState(false);
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [newProjectName, setNewProjectName] = useState("");

  const { favoriteSet, isFavorite, toggleFavorite } = useProjectFavorites(
    organization.id,
  );

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
    navigate(`/${orgSlug}/projects/${result.project.slug}`);
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

  const renderProjectRow = (project: OrgProject) => (
    <ProjectRow
      key={project.id}
      project={project}
      latestLog={latestActionByProjectSlug.get(project.slug)}
      facepile={getFacepileMembers(project.slug)}
      isFavorite={isFavorite(project.id)}
      onToggleFavorite={() => toggleFavorite(project.id)}
    />
  );

  return (
    <>
      <div className="grid grid-cols-1 gap-8 lg:grid-cols-[320px_1fr]">
        <aside className="flex flex-col gap-8 lg:sticky lg:top-4 lg:self-start">
          {isRbacEnabled && <RecentChallengesCompact />}
          <RecentActivityCompact logs={auditLogs} />
        </aside>

        <main className="flex min-w-0 flex-col gap-4">
          <div>
            <Heading variant="h4" className="mb-1">
              Projects
            </Heading>
            <Type small muted>
              Projects organize your MCP servers, skills, assistants, and other
              tools into separate workspaces. Use them to permission and scope
              access to different products, teams or environments within your
              organization.
            </Type>
          </div>

          <div className="flex items-center gap-2">
            <SearchBar
              value={search}
              onChange={setSearch}
              placeholder="Search projects..."
              className="flex-1"
            />
            {canAdmin && (
              <AddNewMenu
                onCreateProject={() => setCreateDialogOpen(true)}
                onInviteMember={() => orgRoutes.team.goTo()}
                onManageRoles={() => orgRoutes.access.roles.goTo()}
              />
            )}
          </div>

          {filteredProjects.length === 0 && isSearching ? (
            <div className="border-border bg-card flex flex-col items-center gap-3 rounded-lg border border-dashed py-12 text-center">
              <Type muted>No projects matching &ldquo;{search}&rdquo;</Type>
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
                <section className="flex flex-col gap-2">
                  <div className="flex items-center gap-2">
                    <Star className="text-foreground size-3.5 fill-current" />
                    <Type small className="text-foreground font-medium">
                      Your favorites
                    </Type>
                  </div>
                  <ProjectList>
                    {favoriteProjects.map(renderProjectRow)}
                  </ProjectList>
                </section>
              )}

              <ProjectList>
                {visibleOtherProjects.map(renderProjectRow)}
                {hasMore && (
                  <button
                    type="button"
                    onClick={() => setExpanded((prev) => !prev)}
                    className="text-muted-foreground hover:text-foreground hover:bg-muted/40 flex items-center justify-center gap-1.5 py-3 text-sm font-medium transition-colors"
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
              </ProjectList>

              {otherProjects.length === 0 && favoriteProjects.length === 0 && (
                <div className="border-border bg-card flex flex-col items-center gap-3 rounded-lg border border-dashed py-12 text-center">
                  <Type muted>No projects yet</Type>
                  <RequireScope scope="org:admin" level="component">
                    <Button size="sm" onClick={() => setCreateDialogOpen(true)}>
                      <Plus className="size-4" />
                      Create your first project
                    </Button>
                  </RequireScope>
                </div>
              )}
            </>
          )}
        </main>
      </div>

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

function AddNewMenu({
  onCreateProject,
  onInviteMember,
  onManageRoles,
}: {
  onCreateProject: () => void;
  onInviteMember: () => void;
  onManageRoles: () => void;
}) {
  const [open, setOpen] = useState(false);
  const handle = (cb: () => void) => () => {
    setOpen(false);
    cb();
  };
  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger asChild>
        <Button className="h-[42px] shrink-0 px-4">
          <Plus className="size-4" />
          Add new
          <ChevronDown className="size-3.5 opacity-70" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-48">
        <DropdownMenuItem onClick={handle(onCreateProject)}>
          <Plus className="size-4" />
          Project
        </DropdownMenuItem>
        <DropdownMenuItem onClick={handle(onInviteMember)}>
          <UserPlus className="size-4" />
          Team member
        </DropdownMenuItem>
        <DropdownMenuItem onClick={handle(onManageRoles)}>
          <ShieldCheck className="size-4" />
          Role
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function ProjectList({ children }: { children: React.ReactNode }) {
  return (
    <div className="border-border bg-card divide-border divide-y overflow-hidden rounded-lg border">
      {children}
    </div>
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

  return (
    <div className="group hover:bg-muted/40 relative flex items-center gap-4 px-4 py-3 transition-colors">
      <Link
        to={`/${orgSlug}/projects/${project.slug}`}
        className="absolute inset-0 z-0"
        aria-label={`Open ${project.name}`}
      />
      <ProjectAvatar
        project={project}
        className="relative z-10 h-9 w-9 shrink-0 rounded-md"
      />

      <div className="relative z-10 flex min-w-0 flex-1 items-center gap-6">
        <div className="w-44 min-w-0 shrink-0">
          <Type
            variant="subheading"
            as="div"
            className="text-foreground truncate text-sm font-medium"
          >
            {project.name}
          </Type>
          <Type small muted className="truncate font-mono text-xs">
            {project.slug}
          </Type>
        </div>

        <div className="hidden min-w-0 flex-1 sm:block">
          <RecentActionBlock log={latestLog} />
        </div>
      </div>

      <Facepile members={facepile} />

      <ProjectRowActions
        project={project}
        isFavorite={isFavorite}
        onToggleFavorite={onToggleFavorite}
      />
    </div>
  );
}

function ProjectRowActions({
  project,
  isFavorite,
  onToggleFavorite,
}: {
  project: OrgProject;
  isFavorite: boolean;
  onToggleFavorite: () => void;
}) {
  const { orgSlug } = useSlugs();
  const navigate = useNavigate();
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
          "hover:bg-muted flex size-8 items-center justify-center rounded-md transition-colors",
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
            className="text-muted-foreground hover:bg-muted hover:text-foreground flex size-8 items-center justify-center rounded-md transition-colors"
          >
            <MoreHorizontal className="size-4" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-48">
          <DropdownMenuItem onClick={closeAnd(onToggleFavorite)}>
            <Star className={cn("size-4", isFavorite && "fill-current")} />
            {isFavorite ? "Remove from favorites" : "Add to favorites"}
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={closeAnd(() =>
              navigate(`/${orgSlug}/projects/${project.slug}/settings`),
            )}
          >
            <Settings className="size-4" />
            Project settings
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={closeAnd(() =>
              navigate(`/${orgSlug}/audit-logs?project=${project.slug}`),
            )}
          >
            <History className="size-4" />
            View audit logs
          </DropdownMenuItem>
          <DropdownMenuItem
            onClick={closeAnd(() => {
              void navigator.clipboard?.writeText(project.slug);
            })}
          >
            <Copy className="size-4" />
            Copy slug
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

function RecentActionBlock({ log }: { log: AuditLog | undefined }) {
  if (!log) {
    return (
      <Type small muted className="text-xs">
        No recent activity
      </Type>
    );
  }
  const actor = getActorLabel(log);
  const verb = renderVerb(log);

  return (
    <div className="relative z-10 flex min-w-0 flex-col gap-0.5">
      <Type
        small
        className="text-foreground truncate text-sm leading-snug font-medium"
      >
        {verb}
      </Type>
      <Tooltip>
        <TooltipTrigger asChild>
          <span className="text-muted-foreground inline-flex w-fit cursor-default text-xs">
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
        <span className="bg-background/20 rounded-sm px-1 py-0.5 text-[10px] uppercase">
          UTC
        </span>
        <span>{utc}</span>
      </div>
      <div className="flex items-center gap-2">
        <span className="bg-background/20 rounded-sm px-1 py-0.5 text-[10px] uppercase">
          {tzAbbr}
        </span>
        <span>{local}</span>
      </div>
    </div>
  );
}

function Facepile({ members }: { members: AccessMember[] }) {
  if (members.length === 0) return null;
  const visible = members.slice(0, FACEPILE_LIMIT);
  const overflow = Math.max(0, members.length - FACEPILE_LIMIT);

  return (
    <div className="relative z-10 hidden shrink-0 items-center md:flex">
      <div className="flex -space-x-1.5">
        {visible.map((member) => (
          <Tooltip key={member.id}>
            <TooltipTrigger asChild>
              <Avatar className="ring-background size-6 ring-2">
                {member.photoUrl ? (
                  <AvatarImage src={member.photoUrl} alt={member.name} />
                ) : null}
                <AvatarFallback className="bg-muted text-muted-foreground text-[10px] font-medium">
                  {getInitials(member.email)}
                </AvatarFallback>
              </Avatar>
            </TooltipTrigger>
            <TooltipContent>{member.name || member.email}</TooltipContent>
          </Tooltip>
        ))}
        {overflow > 0 && (
          <div className="bg-muted text-muted-foreground ring-background flex size-6 items-center justify-center rounded-full text-[10px] font-medium ring-2">
            +{overflow}
          </div>
        )}
      </div>
    </div>
  );
}

function RecentActivityCompact({ logs }: { logs: AuditLog[] }) {
  const orgRoutes = useOrgRoutes();
  const preview = logs.slice(0, AUDIT_PREVIEW_LIMIT);

  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <Heading variant="h4">Recent activity</Heading>
        <orgRoutes.auditLogs.Link className="text-primary text-sm font-medium hover:underline">
          View all
        </orgRoutes.auditLogs.Link>
      </div>
      {preview.length === 0 ? (
        <div className="border-border bg-card rounded-lg border border-dashed px-4 py-6 text-center">
          <Type muted small>
            Activity will appear here as your team makes changes.
          </Type>
        </div>
      ) : (
        <ol className="border-border bg-card divide-border divide-y overflow-hidden rounded-lg border">
          {preview.map((log) => (
            <li
              key={log.id}
              className="flex flex-col gap-1 px-3 py-2.5 text-xs"
            >
              <Type small className="leading-snug">
                <span className="text-foreground font-medium">
                  {getActorLabel(log)}
                </span>{" "}
                <span className="text-muted-foreground">{renderVerb(log)}</span>
              </Type>
              <Type
                muted
                small
                className="text-muted-foreground/80 text-[11px]"
              >
                {log.projectSlug ? `${log.projectSlug} · ` : ""}
                {dateTimeFormatters.humanize(log.createdAt, {
                  includeTime: false,
                })}
              </Type>
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}

function RecentChallengesCompact() {
  const orgRoutes = useOrgRoutes();
  const { hasScope } = useRBAC();
  const canAdmin = hasScope("org:admin");
  const { actionsColumn, grantFlowPortals } = useGrantFlow();
  const { data, isLoading } = useChallengeBuckets({
    outcome: Outcome.Deny,
    resolved: false,
    limit: CHALLENGE_PREVIEW_LIMIT,
  });

  const buckets = useMemo(
    () =>
      (data?.buckets ?? [])
        .filter(isDisplayableBucket)
        .slice(0, CHALLENGE_PREVIEW_LIMIT),
    [data?.buckets],
  );

  const challengeRowColumns = useChallengeRowColumns();
  const columns = useMemo(
    () =>
      canAdmin ? [...challengeRowColumns, actionsColumn] : challengeRowColumns,
    [canAdmin, challengeRowColumns, actionsColumn],
  );

  if (isLoading) return null;

  return (
    <section className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <Heading variant="h4">Recent challenges</Heading>
        <orgRoutes.access.challenges.Link className="text-primary text-sm font-medium hover:underline">
          View all
        </orgRoutes.access.challenges.Link>
      </div>
      {buckets.length === 0 ? (
        <ChallengesEmptyState outcomeFilter="deny" />
      ) : (
        <div className="border-border bg-card overflow-hidden rounded-lg border">
          <Table columns={columns} data={buckets} rowKey={(row) => row.id} />
        </div>
      )}
      {grantFlowPortals}
    </section>
  );
}
