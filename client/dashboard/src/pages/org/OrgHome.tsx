import { Page } from "@/components/page-layout";
import { ProjectAvatar } from "@/components/project-menu";
import { DotCard } from "@/components/ui/dot-card";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { useOrganization } from "@/contexts/Auth";
import { useSlugs } from "@/contexts/Sdk";
import { ArrowRight } from "lucide-react";
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
        <Heading variant="h4" className="mb-1">
          Projects
        </Heading>
        <Type small muted className="mb-4">
          Projects organize your MCP servers, tools, deployments, and
          integrations into separate workspaces. Use them to isolate different
          products or environments within your organization.
        </Type>
        <div className="grid grid-cols-1 xl:grid-cols-2 gap-4">
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
                  className="truncate text-md group-hover:text-primary transition-colors"
                >
                  {project.name}
                </Type>
                <Type small muted className="truncate mb-3">
                  {project.slug}
                </Type>

                <div className="flex items-center justify-end mt-auto pt-2">
                  <div className="flex items-center gap-1 text-muted-foreground group-hover:text-primary transition-colors text-sm">
                    <span>Open</span>
                    <ArrowRight className="w-3.5 h-3.5" />
                  </div>
                </div>
              </DotCard>
            </Link>
          ))}
        </div>
      </Page.Body>
    </Page>
  );
}
