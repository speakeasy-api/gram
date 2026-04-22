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
import { ArrowRight } from "lucide-react";
import { useState } from "react";
import { Link, useNavigate } from "react-router";

export default function OrgHome() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope
          scope={["org:read", "build:read", "org:admin"]}
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
  const [search, setSearch] = useState("");
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [newProjectName, setNewProjectName] = useState("");

  const projects = [...organization.projects]
    .filter((project) => {
      if (!search) return true;
      const query = search.toLowerCase();
      return (
        project.name.toLowerCase().includes(query) ||
        project.slug.toLowerCase().includes(query)
      );
    })
    .sort((a, b) => a.name.localeCompare(b.name));

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
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
          {projects.map((project) => (
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
          <RequireScope scope="org:admin" level="component" className="w-full">
            <CreateResourceCard
              onClick={() => setCreateDialogOpen(true)}
              title="New Project"
              description="Create a new project for your organization"
            />
          </RequireScope>
        </div>
      )}
      {createDialogOpen && (
        <InputDialog
          open={createDialogOpen}
          onOpenChange={setCreateDialogOpen}
          title="Create New Project"
          description="Create a new project to organize your MCP servers, tools, and integrations."
          submitButtonText="Create Project"
          onSubmit={async () => {
            const result = await client.projects.create({
              name: newProjectName,
              organizationId: organization.id,
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
