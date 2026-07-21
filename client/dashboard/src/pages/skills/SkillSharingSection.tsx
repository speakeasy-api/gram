import { RequireScope } from "@/components/require-scope";
import { ErrorAlert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { CopyButton } from "@/components/ui/copy-button";
import { Dialog } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { SettingsSection } from "@/pages/mcp/x/tabs/settings/SettingsSection";
import type { Skill } from "@gram/client/models/components/skill.js";
import { useShareSkillMutation } from "@gram/client/react-query/shareSkill.js";
import { useUnshareSkillMutation } from "@gram/client/react-query/unshareSkill.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { invalidateSkillQueries } from "./invalidate-skill-queries";
import { skillShareUrl } from "./share-link";

type ConfirmAction = "disable" | "reset";

const CONFIRM_COPY: Record<
  ConfirmAction,
  {
    title: string;
    description: string;
    confirmLabel: string;
    confirmingLabel: string;
  }
> = {
  disable: {
    title: "Turn off public sharing?",
    description: "The existing link will stop working immediately.",
    confirmLabel: "Turn off sharing",
    confirmingLabel: "Turning off...",
  },
  reset: {
    title: "Reset public link?",
    description:
      "A new link will be created for this skill. The existing link will stop working immediately.",
    confirmLabel: "Reset link",
    confirmingLabel: "Resetting...",
  },
};

/**
 * Settings section on the skill detail page that toggles the skill's public
 * share link. Sharing is idempotent server-side: repeated shares return the
 * same token, so "reset" is an unshare followed by a fresh share.
 */
export function SkillSharingSection({ skill }: { skill: Skill }): JSX.Element {
  const project = useProject();
  const queryClient = useQueryClient();
  const share = useShareSkillMutation();
  const unshare = useUnshareSkillMutation();
  const [confirmAction, setConfirmAction] = useState<ConfirmAction | null>(
    null,
  );
  const [error, setError] = useState<string | null>(null);

  const pending = share.isPending || unshare.isPending;
  const shareUrl = skill.shareToken ? skillShareUrl(skill.shareToken) : null;

  const enableSharing = async (): Promise<void> => {
    setError(null);
    try {
      await share.mutateAsync({
        request: { shareSkillRequestBody: { skillId: skill.id } },
      });
      await invalidateSkillQueries(queryClient);
      toast.success("Public link enabled");
    } catch (shareError) {
      let message = "Unable to enable public sharing.";
      if (shareError instanceof Error) message = shareError.message;
      setError(message);
      toast.error("Unable to enable public sharing");
    }
  };

  const disableSharing = async (): Promise<void> => {
    setError(null);
    try {
      await unshare.mutateAsync({
        request: { unshareSkillRequestBody: { skillId: skill.id } },
      });
      await invalidateSkillQueries(queryClient);
      setConfirmAction(null);
      toast.success("Public link turned off");
    } catch (unshareError) {
      let message = "Unable to turn off public sharing.";
      if (unshareError instanceof Error) message = unshareError.message;
      setError(message);
      toast.error("Unable to turn off public sharing");
    }
  };

  const resetLink = async (): Promise<void> => {
    setError(null);
    try {
      await unshare.mutateAsync({
        request: { unshareSkillRequestBody: { skillId: skill.id } },
      });
      await share.mutateAsync({
        request: { shareSkillRequestBody: { skillId: skill.id } },
      });
      setConfirmAction(null);
      toast.success("Public link reset");
    } catch (resetError) {
      let message = "Unable to reset the public link.";
      if (resetError instanceof Error) message = resetError.message;
      setError(message);
      toast.error("Unable to reset the public link");
    } finally {
      // Refetch even on failure: the unshare may have succeeded before the
      // share failed, leaving the cached shareToken (and the displayed link)
      // pointing at a revoked URL.
      await invalidateSkillQueries(queryClient);
    }
  };

  const confirmCopy = confirmAction ? CONFIRM_COPY[confirmAction] : null;
  const runConfirmedAction = (): void => {
    if (confirmAction === "disable") void disableSharing();
    if (confirmAction === "reset") void resetLink();
  };

  return (
    <SettingsSection>
      <SettingsSection.Header>
        <SettingsSection.Title>Public sharing</SettingsSection.Title>
        <SettingsSection.Description>
          Publish a read-only page of this skill's manifest that anyone with the
          link can open.
        </SettingsSection.Description>
      </SettingsSection.Header>
      <SettingsSection.Panel>
        <SettingsSection.Body>
          <div className="flex flex-wrap items-center justify-between gap-4">
            <div className="space-y-1">
              <Type
                id="skill-public-share-label"
                className="text-sm font-semibold"
              >
                Share via public link
              </Type>
              <Type small muted className="max-w-xl">
                Anyone with the link can view this skill — no sign-in required.
                The page always shows the latest version.
              </Type>
            </div>
            <RequireScope
              scope="skill:write"
              resourceId={project.id}
              level="component"
            >
              <Switch
                checked={!!skill.shareToken}
                disabled={pending}
                aria-labelledby="skill-public-share-label"
                onCheckedChange={(checked) => {
                  if (checked) {
                    void enableSharing();
                  } else {
                    setConfirmAction("disable");
                  }
                }}
              />
            </RequireScope>
          </div>
          {error && <ErrorAlert title="Sharing update failed" error={error} />}
          {shareUrl && (
            <div className="flex flex-wrap items-center gap-2">
              <Input
                value={shareUrl}
                readOnly
                aria-label="Public link URL"
                className="min-w-64 flex-1 font-mono text-sm"
                onFocus={(event) => event.currentTarget.select()}
              />
              <CopyButton
                text={shareUrl}
                tooltip="Copy public link"
                onCopy={() => {
                  toast.success("Public link copied");
                }}
              />
              <RequireScope
                scope="skill:write"
                resourceId={project.id}
                level="component"
              >
                <Button
                  size="sm"
                  variant="outline"
                  disabled={pending}
                  onClick={() => setConfirmAction("reset")}
                >
                  Reset link
                </Button>
              </RequireScope>
            </div>
          )}
        </SettingsSection.Body>
      </SettingsSection.Panel>

      <Dialog
        open={confirmAction !== null}
        onOpenChange={(open) => {
          if (!open) {
            setError(null);
            setConfirmAction(null);
          }
        }}
      >
        <Dialog.Content>
          <Dialog.Header>
            <Dialog.Title>{confirmCopy?.title}</Dialog.Title>
            <Dialog.Description>{confirmCopy?.description}</Dialog.Description>
          </Dialog.Header>
          <Dialog.Footer>
            <Button variant="outline" onClick={() => setConfirmAction(null)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              disabled={pending || confirmAction === null}
              onClick={runConfirmedAction}
            >
              {pending
                ? confirmCopy?.confirmingLabel
                : confirmCopy?.confirmLabel}
            </Button>
          </Dialog.Footer>
        </Dialog.Content>
      </Dialog>
    </SettingsSection>
  );
}
