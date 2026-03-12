import { Page } from "@/components/page-layout";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useOrganization } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { ProjectAvatar } from "@/components/project-menu";
import { Link } from "react-router";

export default function OrgHome() {
  const organization = useOrganization();
  const { orgSlug } = useSlugs();

  const projects = [...organization.projects].sort((a, b) =>
    a.name.localeCompare(b.name),
  );

  return (
    <Page>
      <Page.Header>
        <Page.Header.Title>Home</Page.Header.Title>
      </Page.Header>
      <Page.Body>
        <Heading variant="h4" className="mb-2">
          Projects
        </Heading>
        <Type muted small className="mb-6">
          Your recent projects across this organization.
        </Type>
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
      </Page.Body>
    </Page>
  );
}
