import { RequireScope } from "@/components/require-scope";
import { useProject } from "@/contexts/Auth";
import { ProjectGuide } from "./ProjectGuide";

export function ProjectGuidePage(): JSX.Element {
  const { id: projectId } = useProject();

  return (
    <RequireScope scope="project:read" resourceId={projectId} level="page">
      <ProjectGuide />
    </RequireScope>
  );
}
