import { DangerSettingsSection } from "@/components/detail/settings-section";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { useProject } from "@/contexts/Auth";
import { useRoutes } from "@/routes";
import { Trash2 } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router";
import { RemoveSourceDialog, type RemovableSource } from "./RemoveSourceDialog";

function SourceSettingRow({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div className="min-w-0 space-y-1">
        <Text small className="font-medium">
          {title}
        </Text>
        <Text muted small className="max-w-2xl">
          {description}
        </Text>
      </div>
      <div className="flex shrink-0 items-center gap-2">{children}</div>
    </div>
  );
}

/**
 * Removing the source. Uploading a new version lives in the page header
 * beside "Build a server", so this section holds only the destructive action.
 */
export function SourceDangerZone({
  source,
}: {
  source: RemovableSource;
}): JSX.Element {
  const routes = useRoutes();
  const navigate = useNavigate();
  const project = useProject();
  const [deleteOpen, setDeleteOpen] = useState(false);

  return (
    <>
      <DangerSettingsSection>
        <DangerSettingsSection.Header>
          <DangerSettingsSection.Title>Danger zone</DangerSettingsSection.Title>
          <DangerSettingsSection.Description>
            Removing this source takes its tools out of every MCP server that
            carries them.
          </DangerSettingsSection.Description>
        </DangerSettingsSection.Header>
        <DangerSettingsSection.Panel>
          <DangerSettingsSection.Body>
            <SourceSettingRow
              title="Delete source"
              description="Remove this source from the project's deployment. This action cannot be undone."
            >
              <RequireScope
                scope="project:write"
                resourceId={project.id}
                level="component"
              >
                <Button
                  variant="destructive-primary"
                  size="md"
                  onClick={() => setDeleteOpen(true)}
                >
                  <Button.LeftIcon>
                    <Trash2 className="size-4" />
                  </Button.LeftIcon>
                  <Button.Text>Delete source</Button.Text>
                </Button>
              </RequireScope>
            </SourceSettingRow>
          </DangerSettingsSection.Body>
        </DangerSettingsSection.Panel>
      </DangerSettingsSection>

      <RemoveSourceDialog
        source={source}
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        onRemoved={() => void navigate(routes.mcp.sources.href())}
      />
    </>
  );
}
