import { SettingsSection } from "@/components/detail/settings-section";
import { Text } from "@/components/ui/Text";
import { dateTimeFormatters, HumanizeDateTime } from "@/lib/dates";
import { useParams } from "react-router";
import { SkillActivitySections } from "./SkillActivitySections";
import { SkillDistributionsSection } from "./SkillDistributionsSection";
import { SkillServedDigest } from "./SkillServedDigest";
import { SkillServedOverMcpSection } from "./SkillServedOverMcpSection";
import { useSkillDetailContext } from "./SkillDetailContext";
import { useSkillsOverMcpEnabled } from "./use-skills-over-mcp";
import { useSkillVersionLabels } from "./use-skill-version-labels";

export default function SkillUsage(): JSX.Element {
  const { skillId = "" } = useParams<{ skillId: string }>();
  const { skillQueryData } = useSkillDetailContext();
  const { latestVersion, assistantCount } = skillQueryData;
  const { versionLabels, versionsLoading } = useSkillVersionLabels(
    skillQueryData.skill.id,
    skillQueryData.skill.versionCount,
  );
  const skillsOverMcp = useSkillsOverMcpEnabled();
  const { lastLoadedOverMcpAt } = skillQueryData.skill;

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
      {skillsOverMcp && (
        <SettingsSection>
          <SettingsSection.Header>
            <SettingsSection.Title>Served over MCP</SettingsSection.Title>
            <SettingsSection.Description>
              Agents connected to these MCP servers and gateways load this skill
              over MCP.{" "}
              {lastLoadedOverMcpAt ? (
                <span
                  title={dateTimeFormatters.full.format(lastLoadedOverMcpAt)}
                >
                  Last loaded <HumanizeDateTime date={lastLoadedOverMcpAt} />.
                </span>
              ) : (
                "Not loaded over MCP yet."
              )}
            </SettingsSection.Description>
            {latestVersion && (
              <Text small muted className="flex flex-wrap items-center gap-x-2">
                Agents loading the current version over MCP see the digest
                <SkillServedDigest rawSha256={latestVersion.rawSha256} />
              </Text>
            )}
          </SettingsSection.Header>
          <SkillServedOverMcpSection
            skillId={skillId}
            skillDisplayName={skillQueryData.skill.displayName}
          />
        </SettingsSection>
      )}
    </>
  );
}
