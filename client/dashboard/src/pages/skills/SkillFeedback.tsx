import { useProject } from "@/contexts/Auth";
import { useParams } from "react-router";
import { SkillFeedbackSection } from "./SkillFeedbackSection";
import { SuggestedSkillEditSection } from "./SuggestedSkillEditSection";
import { useSkillDetailContext } from "./SkillDetailContext";

export default function SkillFeedback(): JSX.Element {
  const project = useProject();
  const { skillId = "" } = useParams<{ skillId: string }>();
  const { skillQueryData } = useSkillDetailContext();
  return (
    <>
      <SkillFeedbackSection skillId={skillId} projectId={project.id} />
      {skillQueryData.latestVersion && (
        <SuggestedSkillEditSection
          skillId={skillId}
          latestVersion={skillQueryData.latestVersion}
        />
      )}
    </>
  );
}
