import { MemberWorkflowCTA } from "@/components/platform-mcp/member-workflow-cta";
import { SkillFeedbackSection } from "./SkillFeedbackSection";
import { SuggestedSkillEditSection } from "./SuggestedSkillEditSection";
import { useParams } from "react-router";
import { useProject } from "@/contexts/Auth";
import { useSkillDetailContext } from "./SkillDetailContext";

export default function SkillFeedback(): JSX.Element {
  const project = useProject();
  const { skillId = "" } = useParams<{ skillId: string }>();
  const { skillQueryData } = useSkillDetailContext();
  return (
    <>
      <MemberWorkflowCTA
        workflow="skill_feedback"
        label="Review feedback in your agent"
        description="Summarise feedback and propose improvements with Platform MCP."
        scope="skill:read"
        resourceId={project.id}
        projectSlug={project.slug}
        prompt={`Using Platform MCP, review feedback and open suggestions for skill ${JSON.stringify(skillQueryData.skill.name)} in project ${JSON.stringify(project.slug)}. Summarise the main problems and propose improvements for me to review. Do not save or approve changes.`}
      />
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
