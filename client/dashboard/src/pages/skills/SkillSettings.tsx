import { DangerSettingsSection } from "@/components/detail/settings-section";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import { useState } from "react";
import { useNavigate } from "react-router";
import {
  ArchiveSkillDialog,
  type ArchiveSkillTarget,
} from "./ArchiveSkillDialog";
import { useSkillDetailContext } from "./SkillDetailContext";

export default function SkillSettings(): JSX.Element {
  const project = useProject();
  const routes = useRoutes();
  const navigate = useNavigate();
  const { skillQueryData } = useSkillDetailContext();
  const { skill } = skillQueryData;
  const [archiveTarget, setArchiveTarget] = useState<ArchiveSkillTarget | null>(
    null,
  );

  return (
    <>
      <DangerSettingsSection>
        <DangerSettingsSection.Header>
          <DangerSettingsSection.Title>Danger zone</DangerSettingsSection.Title>
        </DangerSettingsSection.Header>
        <DangerSettingsSection.Panel>
          <DangerSettingsSection.Body className="flex flex-wrap items-center justify-between gap-2">
            <div className="space-y-1 m-0">
              <Text className="text-sm font-semibold">Archive this skill</Text>
              <Text small muted className="max-w-xl">
                Archiving removes the skill from this project's catalog and
                revokes its plugin distributions.
              </Text>
            </div>
            <RequireScope
              scope="skill:write"
              resourceId={project.id}
              level="component"
            >
              <Button
                variant="destructive-primary"
                onClick={() =>
                  setArchiveTarget({
                    id: skill.id,
                    displayName: skill.displayName,
                  })
                }
              >
                Archive
              </Button>
            </RequireScope>
          </DangerSettingsSection.Body>
        </DangerSettingsSection.Panel>
      </DangerSettingsSection>
      <ArchiveSkillDialog
        skill={archiveTarget}
        onClose={() => setArchiveTarget(null)}
        onArchived={() => void navigate(routes.skills.href())}
      />
    </>
  );
}
