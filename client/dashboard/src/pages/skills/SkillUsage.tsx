import { SettingsSection } from "@/components/detail/settings-section";
import { useParams } from "react-router";
import { SkillActivitySections } from "./SkillActivitySections";
import { SkillDistributionsSection } from "./SkillDistributionsSection";
import { useSkillDetailContext } from "./SkillDetailContext";
import { useSkillVersionLabels } from "./use-skill-version-labels";

export default function SkillUsage(): JSX.Element {
  const { skillId = "" } = useParams<{ skillId: string }>();
  const { skillQueryData } = useSkillDetailContext();
  const { latestVersion, assistantCount } = skillQueryData;
  const { versionLabels, versionsLoading } = useSkillVersionLabels(
    skillQueryData.skill.id,
    skillQueryData.skill.versionCount,
  );

  return (
    <>
      <SkillActivitySections
        data={skillQueryData}
        versionLabels={versionLabels}
        versionsLoading={versionsLoading}
      />
      {latestVersion && (
        <SettingsSection>
          <SettingsSection.Header>
            <SettingsSection.Title>Plugin distributions</SettingsSection.Title>
            <SettingsSection.Description>
              Used by {assistantCount}{" "}
              {assistantCount === 1 ? "assistant" : "assistants"}. The plugins
              carrying this skill ship it inside the plugin package for everyone
              who installs it.
            </SettingsSection.Description>
          </SettingsSection.Header>
          <SkillDistributionsSection skillId={skillId} />
        </SettingsSection>
      )}
    </>
  );
}
