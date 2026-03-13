import { Page } from "@/components/page-layout";
import { Heading } from "@/components/ui/heading";
import { SearchBar } from "@/components/ui/search-bar";
import { Type } from "@/components/ui/type";
import { useOrganization } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { ProjectAvatar } from "@/components/project-menu";
import { useState } from "react";
import { Link } from "react-router";

export default function OrgProjects() {
  const organization = useOrganization();
  const { orgSlug } = useSlugs();
  const [search, setSearch] = useState("");

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
    <Page>
      <Page.Header>
        <Page.Header.Title>Projects</Page.Header.Title>
      </Page.Header>
      <Page.Body>
        <Heading variant="h4" className="mb-2">
          All Projects
        </Heading>
        <Type muted small className="mb-6">
          All projects in your organization.
        </Type>
        <SearchBar
          value={search}
          onChange={setSearch}
          placeholder="Search projects..."
          className="mb-4"
        />
        {projects.length === 0 && search ? (
          <Type muted className="py-8 text-center">
            No projects matching &ldquo;{search}&rdquo;
          </Type>
        ) : projects.length === 0 ? (
          <Type muted className="py-8 text-center">
            No projects yet.
          </Type>
        ) : (
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
            {projects.map((project) => (
              <Link
                key={project.id}
                to={`/${orgSlug}/projects/${project.slug}`}
                className="group rounded-lg border border-border bg-card p-4 hover:border-foreground/20 hover:bg-accent/50 transition-colors hover:no-underline"
              >
                <div className="flex items-center gap-3">
                  <ProjectAvatar
                    project={project}
                    className="h-8 w-8 rounded-md shrink-0"
                  />
                  <div className="min-w-0">
                    <Type
                      variant="body"
                      className="font-medium truncate block text-foreground"
                    >
                      {project.name}
                    </Type>
                    <Type variant="small" className="text-muted-foreground">
                      {project.slug}
                    </Type>
                  </div>
                </div>
              </Link>
            ))}
          </div>
        )}
      </Page.Body>
    </Page>
  );
}
