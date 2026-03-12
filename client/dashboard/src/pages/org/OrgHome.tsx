import { Page } from "@/components/page-layout";
import { getProjectColors, ProjectAvatar } from "@/components/project-menu";
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
        <Heading variant="h4" className="mb-2">
          Projects
        </Heading>
        <Type muted small className="mb-6">
          Your projects across this organization.
        </Type>
        <div className="grid grid-cols-1 xl:grid-cols-2 gap-4">
          {projects.map((project) => {
            const colors = getProjectColors(project.id);
            return (
              <Link
                key={project.id}
                to={`/${orgSlug}/projects/${project.slug}`}
                className="group bg-card text-card-foreground flex flex-row rounded-xl border !border-foreground/10 overflow-hidden hover:!border-foreground/30 hover:shadow-md transition-all h-full hover:no-underline"
              >
                {/* Illustration sidebar with dot pattern */}
                <div className="w-32 shrink-0 overflow-hidden border-r relative text-muted-foreground/20">
                  <div
                    className="absolute inset-0"
                    style={{
                      backgroundColor: `color-mix(in srgb, ${colors.from} 8%, transparent)`,
                      backgroundImage:
                        "radial-gradient(circle, currentColor 1px, transparent 1px)",
                      backgroundSize: "16px 16px",
                    }}
                  />
                  <div className="absolute inset-0 flex items-center justify-center">
                    <div className="bg-background/90 backdrop-blur-sm rounded-lg p-3 shadow-lg">
                      <ProjectAvatar
                        project={project}
                        className="h-10 w-10 rounded-md"
                      />
                    </div>
                  </div>
                </div>

                {/* Content area */}
                <div className="p-4 flex flex-col flex-1 min-w-0">
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
                </div>
              </Link>
            );
          })}
        </div>
      </Page.Body>
    </Page>
  );
}
