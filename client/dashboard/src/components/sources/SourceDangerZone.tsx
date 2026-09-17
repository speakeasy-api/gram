import {
  DangerSettingsSection,
  SettingsSection,
} from "@/components/detail/settings-section";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import { Trash2, Upload } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router";
import { RemoveSourceDialog, type RemovableSource } from "./RemoveSourceDialog";
import { newVersionQueryParams } from "./source-list-actions";

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
 * What changes a source: a new version of the document, or its removal.
 *
 * Only OpenAPI documents take a new version here; functions are pushed from
 * the CLI, and a push is their upload.
 */
export function SourceDangerZone({
  source,
  slug,
}: {
  source: RemovableSource;
  /** The deployment's slug for the source, which the upload flow keys on. */
  slug: string | undefined;
}): JSX.Element {
  const routes = useRoutes();
  const navigate = useNavigate();
  const [deleteOpen, setDeleteOpen] = useState(false);

  return (
    <div className="flex flex-col gap-8">
      {source.kind === "openapi" && slug && (
        <SettingsSection>
          <SettingsSection.Header>
            <SettingsSection.Title>Manage</SettingsSection.Title>
          </SettingsSection.Header>
          <SettingsSection.Panel>
            <SettingsSection.Body>
              <SourceSettingRow
                title="Upload a new version"
                description="Replace this document with a newer one. Tools are regenerated from it on the next deployment."
              >
                <RequireScope scope="project:write" level="component">
                  <Button variant="secondary" size="md" asChild>
                    <routes.mcp.add.openapi.Link
                      queryParams={newVersionQueryParams(slug)}
                    >
                      <Button.LeftIcon>
                        <Upload className="size-4" />
                      </Button.LeftIcon>
                      <Button.Text>Upload new version</Button.Text>
                    </routes.mcp.add.openapi.Link>
                  </Button>
                </RequireScope>
              </SourceSettingRow>
            </SettingsSection.Body>
          </SettingsSection.Panel>
        </SettingsSection>
      )}

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
              <RequireScope scope="project:write" level="component">
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
    </div>
  );
}
