import { SettingsSection } from "@/components/detail/settings-section";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { Markdown } from "@/elements/components/Markdown";
import { HumanizeDateTime } from "@/lib/dates";
import { useParams } from "react-router";
import { useState } from "react";
import type { SkillVersion } from "@gram/client/models/components/skillversion.js";
import { SkillManifestDialog } from "./SkillManifestDialog";
import { SkillValidationErrors } from "./SkillValidationErrors";
import { stripSkillFrontmatter } from "./skill-manifest";
import { useSkillDetailContext } from "./SkillDetailContext";

export default function SkillContent(): JSX.Element {
  const project = useProject();
  const { skillId = "" } = useParams<{ skillId: string }>();
  const { skillQueryData } = useSkillDetailContext();
  const { skill, latestVersion } = skillQueryData;
  const [editOpen, setEditOpen] = useState(false);
  const body = latestVersion
    ? stripSkillFrontmatter(latestVersion.content)
    : "";
  const frontmatterEntries = Object.entries(
    latestVersion?.frontmatter ?? {},
  ).filter(([key]) => key !== "name" && key !== "description");

  return (
    <>
      <SettingsSection>
        <SettingsSection.Header>
          <SettingsSection.Title>SKILL.md</SettingsSection.Title>
          <SettingsSection.Description>
            The current version of this skill's manifest, exactly as agents load
            it.
          </SettingsSection.Description>
        </SettingsSection.Header>
        <SettingsSection.Panel>
          <SettingsSection.Body>
            {latestVersion && !latestVersion.specValid && (
              <ValidationErrors errors={latestVersion.validationErrors} />
            )}
            <div className="overflow-x-auto">
              {latestVersion ? (
                <ManifestBody body={body} />
              ) : (
                <Text small muted>
                  Manifest content has not been captured for this observed
                  skill.
                </Text>
              )}
            </div>
          </SettingsSection.Body>
          {latestVersion && (
            <SettingsSection.Footer>
              <SettingsSection.FooterHint>
                Current version{" "}
                <span className="font-mono">
                  {latestVersion.canonicalSha256.slice(0, 8)}
                </span>{" "}
                · updated <HumanizeDateTime date={skill.updatedAt} />
              </SettingsSection.FooterHint>
              <SettingsSection.FooterActions>
                <RequireScope
                  scope="skill:write"
                  resourceId={project.id}
                  level="component"
                >
                  <Button size="sm" onClick={() => setEditOpen(true)}>
                    Edit SKILL.md
                  </Button>
                </RequireScope>
              </SettingsSection.FooterActions>
            </SettingsSection.Footer>
          )}
        </SettingsSection.Panel>
      </SettingsSection>
      {frontmatterEntries.length > 0 && (
        <SettingsSection>
          <SettingsSection.Header>
            <SettingsSection.Title>Frontmatter</SettingsSection.Title>
            <SettingsSection.Description>
              Additional metadata declared in the manifest's frontmatter.
            </SettingsSection.Description>
          </SettingsSection.Header>
          <SettingsSection.Panel>
            <SettingsSection.Body>
              <dl className="space-y-1.5">
                {frontmatterEntries.map(([key, value]) => (
                  <div key={key} className="flex gap-3 text-sm">
                    <dt className="text-muted-foreground shrink-0 font-mono">
                      {key}
                    </dt>
                    <dd className="min-w-0 break-words font-mono">
                      {typeof value === "string"
                        ? value
                        : JSON.stringify(value)}
                    </dd>
                  </div>
                ))}
              </dl>
            </SettingsSection.Body>
          </SettingsSection.Panel>
        </SettingsSection>
      )}
      {latestVersion && (
        <SkillManifestDialog
          key={editOpen ? "edit" : "closed"}
          mode="edit"
          open={editOpen}
          onOpenChange={setEditOpen}
          skillId={skillId}
          derivedFromVersionId={latestVersion.id}
          initialContent={latestVersion.content}
        />
      )}
    </>
  );
}

function ManifestBody({ body }: { body: string }): JSX.Element {
  if (body.trim().length === 0) {
    return (
      <Text small muted>
        This manifest has no Markdown body.
      </Text>
    );
  }
  return <Markdown className="text-sm">{body}</Markdown>;
}

function ValidationErrors({
  errors,
}: {
  errors: SkillVersion["validationErrors"];
}): JSX.Element {
  return (
    <div className="border-destructive/40 bg-destructive/5 border p-4">
      <Text variant="subheading" className="text-destructive mb-2">
        Current version has validation issues
      </Text>
      <SkillValidationErrors errors={errors} />
    </div>
  );
}
