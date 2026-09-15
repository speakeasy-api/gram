import { useParams } from "react-router";
import { ScoredSessions } from "./SkillInsightsSection";
import { useSkillDetailContext } from "./SkillDetailContext";
import { useSkillVersionLabels } from "./use-skill-version-labels";

export default function SkillScoredSessions(): JSX.Element {
  const { skillId = "" } = useParams<{ skillId: string }>();
  const { skillQueryData } = useSkillDetailContext();
  const { versionLabels } = useSkillVersionLabels(
    skillQueryData.skill.id,
    skillQueryData.skill.versionCount,
  );
  return <ScoredSessions skillId={skillId} versionLabels={versionLabels} />;
}
