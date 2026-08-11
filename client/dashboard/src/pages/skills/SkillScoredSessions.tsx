import { useParams } from "react-router";
import { ScoredSessions } from "./SkillInsightsSection";
import { useSkillDetailContext } from "./SkillDetailContext";

export default function SkillScoredSessions(): JSX.Element {
  const { skillId = "" } = useParams<{ skillId: string }>();
  const { versionLabels } = useSkillDetailContext();
  return <ScoredSessions skillId={skillId} versionLabels={versionLabels} />;
}
