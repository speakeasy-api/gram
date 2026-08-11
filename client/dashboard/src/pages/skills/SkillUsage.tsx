import { SettingsSection } from "@/components/detail/settings-section";
import { useParams } from "react-router";
import { SkillActivitySections } from "./SkillActivitySections";
import { SkillDistributionsSection } from "./SkillDistributionsSection";
import { useSkillDetailContext } from "./SkillDetailContext";

export default function SkillUsage(): JSX.Element {
  const { skillId = "" } = useParams<{ skillId: string }>();
  const { skillQueryData, versionLabels, versionsLoading } =
    useSkillDetailContext();
  const { latestVersion, assistantCount } = skillQueryData;

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
