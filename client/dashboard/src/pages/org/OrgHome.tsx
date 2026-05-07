import { InputDialog } from "@/components/input-dialog";
import { Page } from "@/components/page-layout";
import { ProjectAvatar } from "@/components/project-menu";
import { CreateResourceCard } from "@/components/create-resource-card";
import { DotCard } from "@/components/ui/dot-card";
import { Heading } from "@/components/ui/heading";
import { SearchBar } from "@/components/ui/search-bar";
import { Type } from "@/components/ui/type";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient, useSlugs } from "@/contexts/Sdk";
import { RequireScope } from "@/components/require-scope";
import { useTelemetry } from "@/contexts/Telemetry";
import { useOrgRoutes } from "@/routes";
import { useRBAC } from "@/hooks/useRBAC";
import { useChallenges } from "@gram/client/react-query/challenges.js";
import { useChallengeRowColumns } from "@/pages/access/useChallengeRowColumns";
import { useGrantFlow } from "@/pages/access/useGrantFlow";
import { Table } from "@speakeasy-api/moonshine";
import { ArrowRight, ChevronDown, ChevronUp } from "lucide-react";
import { useMemo, useState } from "react";
import { Link, useNavigate } from "react-router";

const PROJECT_LIMIT = 5;

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
  const isRbacEnabled = telemetry.isFeatureEnabled("gram-rbac") ?? false;
  const [search, setSearch] = useState("");
  const [expanded, setExpanded] = useState(false);
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [newProjectName, setNewProjectName] = useState("");

  const projects = useMemo(
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

  // When searching, show all results; otherwise cap at limit
  const isSearching = search.length > 0;
  const hasMore = !isSearching && projects.length > PROJECT_LIMIT;
  const visibleProjects =
    expanded || isSearching ? projects : projects.slice(0, PROJECT_LIMIT);

  return (
    <>
      <Heading variant="h4" className="mb-1">
        Projects
      </Heading>
      <Type small muted className="mb-4">
        Projects organize your MCP servers, tools, deployments, and integrations
        into separate workspaces. Use them to isolate different products or
        environments within your organization.
      </Type>
      <SearchBar
        value={search}
        onChange={setSearch}
        placeholder="Search projects..."
        className="mb-4"
      />
      {projects.length === 0 && search ? (
        <div className="space-y-8 pt-6">
          <Type muted className="text-center">
            No projects matching &ldquo;{search}&rdquo;
          </Type>
          <div className="flex justify-center">
            <RequireScope
              scope="org:admin"
              level="component"
              className="w-full"
            >
              <CreateResourceCard
                className="max-w-md"
                onClick={() => {
                  setNewProjectName(search);
                  setCreateDialogOpen(true);
                }}
                title={<>Create &ldquo;{search}&rdquo;</>}
                description="Create a new project with this name"
              />
            </RequireScope>
          </div>
        </div>
      ) : (
        <>
          <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
            {!isSearching && (
              <RequireScope
                scope="org:admin"
                level="component"
                className="w-full"
              >
                <CreateResourceCard
                  onClick={() => setCreateDialogOpen(true)}
                  title="New Project"
                  description="Create a new project for your organization"
                />
              </RequireScope>
            )}
            {visibleProjects.map((project) => (
              <Link
                key={project.id}
                to={`/${orgSlug}/projects/${project.slug}`}
                className="hover:no-underline"
              >
                <DotCard
                  icon={
                    <ProjectAvatar
                      project={project}
                      className="h-10 w-10 rounded-md"
                    />
                  }
                >
                  <Type
                    variant="subheading"
                    as="div"
                    className="text-md group-hover:text-primary truncate transition-colors"
                  >
                    {project.name}
                  </Type>
                  <Type small muted className="mb-3 truncate">
                    {project.slug}
                  </Type>

                  <div className="mt-auto flex items-center justify-end pt-2">
                    <div className="text-muted-foreground group-hover:text-primary flex items-center gap-1 text-sm transition-colors">
                      <span>Open</span>
                      <ArrowRight className="h-3.5 w-3.5" />
                    </div>
                  </div>
                </DotCard>
              </Link>
            ))}
          </div>
          {hasMore && (
            <button
              type="button"
              onClick={() => setExpanded((prev) => !prev)}
              className="text-muted-foreground hover:text-foreground mx-auto mt-4 flex items-center gap-1.5 text-sm font-medium transition-colors"
            >
              {expanded ? (
                <>
                  Show less
                  <ChevronUp className="h-4 w-4" />
                </>
              ) : (
                <>
                  Show all {projects.length} projects
                  <ChevronDown className="h-4 w-4" />
                </>
              )}
            </button>
          )}
        </>
      )}

      {isRbacEnabled && <RecentChallenges />}

      {createDialogOpen && (
        <InputDialog
          open={createDialogOpen}
          onOpenChange={setCreateDialogOpen}
          title="Create New Project"
          description="Create a new project to organize your MCP servers, tools, and integrations."
          submitButtonText="Create Project"
          onSubmit={async () => {
            const result = await client.projects.create({
              createProjectRequestBody: {
                name: newProjectName,
                organizationId: organization.id,
              },
            });
            setNewProjectName("");
            navigate(`/${orgSlug}/projects/${result.project.slug}`);
          }}
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

function RecentChallenges() {
  const orgRoutes = useOrgRoutes();
  const { hasScope } = useRBAC();
  const canAdmin = hasScope("org:admin");
  const { actionsColumn, grantFlowPortals } = useGrantFlow();
  const challengeRowColumns = useChallengeRowColumns();
  const { data: challengesData } = useChallenges({ limit: 5 });
  const recentChallenges = (challengesData?.challenges ?? []).filter(
    (c) => !!c.scope,
  );

  const columns = useMemo(
    () =>
      canAdmin ? [...challengeRowColumns, actionsColumn] : challengeRowColumns,
    [canAdmin, challengeRowColumns, actionsColumn],
  );

  if (recentChallenges.length === 0) return null;

  return (
    <div className="mt-12">
      <div className="mb-3 flex items-center justify-between">
        <Heading variant="h4">Recent Challenges</Heading>
        <orgRoutes.access.challenges.Link className="text-primary cursor-pointer text-sm font-medium hover:underline">
          Show more
        </orgRoutes.access.challenges.Link>
      </div>
      <Table
        columns={columns}
        data={recentChallenges}
        rowKey={(row) => row.id}
      />
      {grantFlowPortals}
    </div>
  );
}
