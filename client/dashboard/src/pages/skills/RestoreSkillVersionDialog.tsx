import { ErrorAlert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import type { SkillVersion } from "@gram/client/models/components/skillversion.js";
import { useRestoreSkillVersionMutation } from "@gram/client/react-query/restoreSkillVersion.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { invalidateSkillQueries } from "./invalidate-skill-queries";
import type { VersionChangeDirection } from "./version-selection";
import { diffSkillFiles, summarizeFileChanges } from "./skill-files";
import { Text } from "@/components/ui/Text";

const MAX_LISTED_FILE_CHANGES = 8;

/** Lists the supporting files a version change adds, removes, or changes. */
function FileImpact({
  from,
  to,
}: {
  from: SkillVersion;
  to: SkillVersion;
}): JSX.Element | null {
  const changes = diffSkillFiles(from.files, to.files).filter(
    (change) => change.status !== "unchanged",
  );
  if (changes.length === 0) return null;
  const { added, removed, changed } = summarizeFileChanges(changes);
  const parts = [
    added > 0 && `${added} added`,
    removed > 0 && `${removed} removed`,
    changed > 0 && `${changed} changed`,
  ].filter(Boolean);
  const listed = changes.slice(0, MAX_LISTED_FILE_CHANGES);
  return (
    <div className="space-y-2">
      <Text small>Supporting files also change: {parts.join(", ")}.</Text>
      <ul className="space-y-0.5">
        {listed.map((change) => (
          <li key={change.path} className="flex gap-3 text-sm">
            <span className="text-muted-foreground w-16 shrink-0 font-mono text-xs uppercase">
              {change.status}
            </span>
            <span className="min-w-0 truncate font-mono" title={change.path}>
              {change.path}
            </span>
          </li>
        ))}
      </ul>
      {changes.length > listed.length && (
        <Text small muted>
          And {changes.length - listed.length} more.
        </Text>
      )}
    </div>
  );
}

export function RestoreSkillVersionDialog({
  skillId,
  version,
  direction,
  currentVersion,
  onClose,
}: {
  skillId: string;
  version: SkillVersion | null;
  direction: VersionChangeDirection | null;
  /** The version that is current now, to show which files the change touches. */
  currentVersion?: SkillVersion;
  onClose: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const restore = useRestoreSkillVersionMutation();
  const [error, setError] = useState<string | null>(null);
  const [reconciling, setReconciling] = useState(false);
  const [uncertain, setUncertain] = useState(false);

  const closeDialog = (): void => {
    if (reconciling) return;
    setError(null);
    setUncertain(false);
    onClose();
  };

  const restoreVersion = async (target: SkillVersion): Promise<void> => {
    setError(null);
    setReconciling(true);
    let restored = false;
    try {
      await restore.mutateAsync({
        request: {
          restoreSkillVersionRequestBody: {
            id: skillId,
            versionId: target.id,
          },
        },
      });
      restored = true;
    } catch (restoreError) {
      const message =
        restoreError instanceof Error
          ? restoreError.message
          : "Unable to change the current version.";
      setError(
        `${message} The current version may be unknown. Review the refreshed state before retrying.`,
      );
      setUncertain(true);
    } finally {
      await invalidateSkillQueries(queryClient).catch(() => undefined);
      setReconciling(false);
    }
    if (!restored) return;
    setError(null);
    setUncertain(false);
    onClose();
    toast.success(
      `Version ${target.canonicalSha256.slice(0, 8)} is now current`,
    );
  };

  const actionLabel = direction === "backward" ? "Roll back" : "Promote";

  return (
    <Dialog
      open={version !== null && direction !== null}
      onOpenChange={(open) => {
        if (!open) closeDialog();
      }}
    >
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>{actionLabel} to this skill version?</Dialog.Title>
          <Dialog.Description>
            This makes version {version?.canonicalSha256.slice(0, 8)} current.
            Explicit distribution pins for plugins and assistants stay
            unchanged.
          </Dialog.Description>
        </Dialog.Header>
        {version && currentVersion && (
          <FileImpact from={currentVersion} to={version} />
        )}
        {error && (
          <ErrorAlert title="Version change status unknown" error={error} />
        )}
        <Dialog.Footer>
          <Button
            variant="secondary"
            disabled={reconciling}
            onClick={closeDialog}
          >
            Cancel
          </Button>
          <Button
            disabled={
              reconciling || uncertain || version === null || direction === null
            }
            onClick={() => {
              if (version) void restoreVersion(version);
            }}
          >
            {reconciling ? `${actionLabel}...` : actionLabel}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
