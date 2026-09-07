import { RequireScope } from "@/components/require-scope";
import { ProjectGuide } from "./ProjectGuide";

export function ProjectGuidePage(): JSX.Element {
  return (
    <RequireScope scope="project:read" level="page">
      <ProjectGuide />
    </RequireScope>
  );
}
