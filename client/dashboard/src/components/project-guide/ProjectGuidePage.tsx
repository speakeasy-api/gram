import { RequireScope } from "@/components/require-scope";
import { useProject } from "@/contexts/Auth";
import { ProjectGuide } from "./ProjectGuide";
import * as authz from "@/lib/authz.gen";

export function ProjectGuidePage(): JSX.Element {
  const project = useProject();
  return (
    <RequireScope check={authz.project(project.id).read()} level="page">
      <ProjectGuide />
    </RequireScope>
  );
}
