import { ProjectAvatar } from "@/components/project-menu";
import { Card } from "@/components/ui/Card";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { getPreferredProject } from "@/lib/preferredProject";
import { ChevronRight } from "lucide-react";
import { Link, Navigate } from "react-router";

/**
 * Shown in place of the org home when the viewer can't see it. The session's
 * project list already holds only the projects the viewer can read, so a
 * member with project-only access goes straight to one of them.
 */
export function OrgHomeNoAccess(): JSX.Element {
  const organization = useOrganization();
  const projects = organization.projects;
  const target =
    getPreferredProject(projects) ??
    projects.find((p) => p.slug === "default") ??
    (projects.length === 1 ? projects[0] : undefined);

  if (target) {
    return (
      <Navigate replace to={`/${organization.slug}/projects/${target.slug}`} />
    );
  }

  return (
    <div className="mx-auto flex w-full max-w-xl flex-col gap-6 px-4 py-16">
      {projects.length === 0 ? (
        <div className="space-y-2 text-center">
          <Text variant="subheading">You're not in any projects yet</Text>
          <Text muted>
            Ask an admin of {organization.name} to add you to a project.
          </Text>
        </div>
      ) : (
        <>
          <div className="space-y-2">
            <Text variant="subheading">Choose a project</Text>
            <Text muted>These are the projects you can open.</Text>
          </div>
          <Card className="divide-border divide-y p-0">
            {projects.map((project) => (
              <Link
                key={project.id}
                to={`/${organization.slug}/projects/${project.slug}`}
                className="hover:bg-muted/50 flex items-center gap-3 px-4 py-3 no-underline"
              >
                <ProjectAvatar project={project} className="size-6" />
                <span className="flex-1 truncate text-sm font-medium">
                  {project.name}
                </span>
                <ChevronRight className="text-muted-foreground size-4" />
              </Link>
            ))}
          </Card>
        </>
      )}
    </div>
  );
}
