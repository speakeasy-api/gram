import { McpSidebarInfoLabel } from "@/components/mcp-sidebar-nav-shell";
import { Button } from "@/components/ui/button";
import { CopyButton } from "@/components/ui/copy-button";
import { Dialog } from "@/components/ui/dialog";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useRBAC } from "@/hooks/useRBAC";
import { cn } from "@/lib/utils";
import type { Skill } from "@gram/client/models/components/skill.js";
import { useShareSkillMutation } from "@gram/client/react-query/shareSkill.js";
import { useUnshareSkillMutation } from "@gram/client/react-query/unshareSkill.js";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Check, ChevronDown, RotateCcw } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { invalidateSkillQueries } from "./invalidate-skill-queries";
import { skillShareUrl } from "./share-link";

type SharingStatus = "private" | "public";

type ConfirmAction = "disable" | "reset";

const STATUS_OPTIONS: {
  value: SharingStatus;
  label: string;
  description: string;
  dotClass: string;
  hoverDotClass: string;
}[] = [
  {
    value: "private",
    label: "Private",
    description: "Only members of this project can view this skill.",
    dotClass: "bg-blue-400",
    hoverDotClass: "group-hover:bg-blue-400",
  },
  {
    value: "public",
    label: "Public",
    description:
      "Anyone with the link can view a read-only page of this skill's manifest. The page always shows the latest version.",
    dotClass: "bg-green-400",
    hoverDotClass: "group-hover:bg-green-400",
  },
];

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
    title: "Make this skill private?",
    description: "The existing public link will stop working immediately.",
    confirmLabel: "Make private",
    confirmingLabel: "Making private...",
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
 * Sidebar-card sharing controls for the skill detail page, mirroring the MCP
 * details card: a Visibility dropdown (Private / Public) and, when public, the
 * share URL with copy and reset actions. Sharing is idempotent server-side:
 * repeated shares return the same token, so "reset" is an unshare followed by
 * a fresh share.
 */
export function SkillSharingCardBlocks({
  skill,
}: {
  skill: Skill;
}): JSX.Element {
  const { hasScope } = useRBAC();
  const canWrite = hasScope("skill:write");
  const queryClient = useQueryClient();
  const share = useShareSkillMutation();
  const unshare = useUnshareSkillMutation();
  const [dropdownOpen, setDropdownOpen] = useState(false);
  const [confirmAction, setConfirmAction] = useState<ConfirmAction | null>(
    null,
  );

  const pending = share.isPending || unshare.isPending;
  const shareUrl = skill.shareToken ? skillShareUrl(skill.shareToken) : null;
  const currentStatus: SharingStatus = shareUrl ? "public" : "private";
  const currentOption = STATUS_OPTIONS.find(
    (option) => option.value === currentStatus,
  );

  const enableSharing = async (): Promise<void> => {
    try {
      await share.mutateAsync({
        request: { shareSkillRequestBody: { skillId: skill.id } },
      });
      toast.success("Public link enabled");
    } catch {
      toast.error("Unable to enable public sharing");
    } finally {
      // Refetch even on failure: the share may have succeeded server-side
      // (e.g. a lost response), leaving the cached state out of date.
      await invalidateSkillQueries(queryClient);
    }
  };

  const disableSharing = async (): Promise<void> => {
    try {
      await unshare.mutateAsync({
        request: { unshareSkillRequestBody: { skillId: skill.id } },
      });
      setConfirmAction(null);
      toast.success("Skill set to private");
    } catch {
      toast.error("Unable to turn off public sharing");
    } finally {
      // Refetch even on failure: the unshare may have succeeded server-side
      // (e.g. a lost response), leaving the displayed link pointing at a
      // revoked URL.
      await invalidateSkillQueries(queryClient);
    }
  };

  const resetLink = async (): Promise<void> => {
    try {
      await unshare.mutateAsync({
        request: { unshareSkillRequestBody: { skillId: skill.id } },
      });
      await share.mutateAsync({
        request: { shareSkillRequestBody: { skillId: skill.id } },
      });
      setConfirmAction(null);
      toast.success("Public link reset");
    } catch {
      toast.error("Unable to reset the public link");
    } finally {
      // Refetch even on failure: the unshare may have succeeded before the
      // share failed, leaving the cached shareToken (and the displayed link)
      // pointing at a revoked URL.
      await invalidateSkillQueries(queryClient);
    }
  };

  const handleSelect = (status: SharingStatus): void => {
    if (status === currentStatus) return;
    setDropdownOpen(false);

    if (status === "public") {
      void enableSharing();
      return;
    }
    // Defer the dialog until the dropdown has fully closed to avoid Radix
    // focus-trap conflicts (same pattern as MCPStatusDropdown).
    setTimeout(() => {
      setConfirmAction("disable");
    }, 0);
  };

  const confirmCopy = confirmAction ? CONFIRM_COPY[confirmAction] : null;
  const runConfirmedAction = (): void => {
    if (confirmAction === "disable") void disableSharing();
    if (confirmAction === "reset") void resetLink();
  };

  return (
    <>
      <div className="flex flex-col gap-1.5">
        <McpSidebarInfoLabel>Visibility</McpSidebarInfoLabel>
        <DropdownMenu open={dropdownOpen} onOpenChange={setDropdownOpen}>
          <DropdownMenuTrigger asChild disabled={!canWrite || pending}>
            <button
              type="button"
              disabled={!canWrite || pending}
              className="text-foreground hover:bg-muted trans border-border flex w-fit items-center gap-2 rounded-md border px-3 py-1.5 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
            >
              <span
                className={cn(
                  "h-2 w-2 shrink-0 rounded-full",
                  currentOption?.dotClass,
                )}
              />
              {currentOption?.label}
              <ChevronDown className="text-muted-foreground h-3 w-3" />
            </button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-[320px] p-1">
            {STATUS_OPTIONS.map((option) => (
              <DropdownMenuItem
                key={option.value}
                onSelect={() => handleSelect(option.value)}
                className="group flex cursor-pointer items-start gap-2.5 rounded-md p-2"
              >
                {option.value === currentStatus ? (
                  <span
                    className={cn(
                      "mt-1 flex size-3.5 shrink-0 items-center justify-center rounded-full",
                      option.dotClass,
                    )}
                  >
                    <Check
                      className="text-background h-2.5 w-2.5"
                      strokeWidth={4}
                    />
                  </span>
                ) : (
                  <span
                    className={cn(
                      "mt-1 size-3.5 shrink-0 rounded-full transition-colors",
                      "bg-muted",
                      option.hoverDotClass,
                    )}
                  />
                )}
                <div className="flex-1">
                  <span className="block font-mono text-xs font-semibold tracking-wide uppercase">
                    {option.label}
                  </span>
                  <span className="text-muted-foreground text-xs">
                    {option.description}
                  </span>
                </div>
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {shareUrl && (
        <div className="flex flex-col gap-1">
          <McpSidebarInfoLabel>Public link</McpSidebarInfoLabel>
          <div className="flex items-start gap-1">
            <Type
              variant="small"
              muted
              className="line-clamp-2 font-mono text-xs break-all"
            >
              {shareUrl.replace(/^https?:\/\//, "")}
            </Type>
            <CopyButton
              text={shareUrl}
              size="inline"
              tooltip="Copy public link"
              className="mt-[-2px] shrink-0"
              onCopy={() => {
                toast.success("Public link copied");
              }}
            />
            {canWrite && (
              <SimpleTooltip tooltip="Reset link">
                <Button
                  size="icon-sm"
                  variant="ghost"
                  disabled={pending}
                  aria-label="Reset public link"
                  className="mt-[-4px] shrink-0"
                  onClick={() => setConfirmAction("reset")}
                >
                  <RotateCcw className="h-3.5 w-3.5" />
                </Button>
              </SimpleTooltip>
            )}
          </div>
        </div>
      )}

      <Dialog
        open={confirmAction !== null}
        onOpenChange={(open) => {
          if (!open) setConfirmAction(null);
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
    </>
  );
}
