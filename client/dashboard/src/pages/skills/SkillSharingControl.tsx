import { DETAIL_HEADER_CONTROL_BUTTON } from "@/components/detail/detail-frame";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Dialog } from "@/components/ui/Dialog";
import { StatusDot } from "@/components/ui/StatusDot";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { cn } from "@/lib/utils";
import type { Skill } from "@gram/client/models/components/skill.js";
import { useShareSkillMutation } from "@gram/client/react-query/shareSkill.js";
import { useUnshareSkillMutation } from "@gram/client/react-query/unshareSkill.js";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { useQueryClient } from "@tanstack/react-query";
import { Check, ChevronDown, RotateCcw } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { useCustomDomain } from "@/hooks/useToolsetUrl";
import { invalidateSkillQueries } from "./invalidate-skill-queries";
import { skillShareDomain, skillShareUrl } from "./share-link";

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
 * The skill's visibility, as a header control on the skill detail page: a
 * Private / Public picker in the header button look. When public, the same
 * menu copies or resets the share link. Sharing is idempotent server-side:
 * repeated shares return the same token, so "reset" is an unshare followed by
 * a fresh share.
 */
export function SkillVisibilityControl({
  skill,
}: {
  skill: Skill;
}): JSX.Element {
  const project = useProject();
  const { hasScope } = useRBAC();
  const canWrite = hasScope("skill:write", project.id);
  const queryClient = useQueryClient();
  const share = useShareSkillMutation();
  const unshare = useUnshareSkillMutation();
  const [dropdownOpen, setDropdownOpen] = useState(false);
  const [confirmAction, setConfirmAction] = useState<ConfirmAction | null>(
    null,
  );

  const pending = share.isPending || unshare.isPending;
  // Public pages are served on the org's custom domain when one is live, so
  // the copied link carries the customer's own branding.
  const { domain: customDomain } = useCustomDomain(!!skill.shareToken);
  const shareUrl = skill.shareToken
    ? skillShareUrl(skill.shareToken, skillShareDomain(customDomain))
    : null;
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
      <DropdownMenu open={dropdownOpen} onOpenChange={setDropdownOpen}>
        <DropdownMenuTrigger asChild disabled={!canWrite || pending}>
          <button
            type="button"
            disabled={!canWrite || pending}
            aria-label={`Visibility: ${currentOption?.label ?? ""}`}
            className={DETAIL_HEADER_CONTROL_BUTTON}
          >
            <StatusDot
              tone={currentStatus === "public" ? "success" : "information"}
            />
            <span className="min-w-0 flex-1 truncate text-left">
              {currentOption?.label}
            </span>
            <ChevronDown className="text-muted-foreground size-3.5 shrink-0" />
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" className="w-[320px] p-1">
          {STATUS_OPTIONS.map((option) => (
            <DropdownMenuItem
              key={option.value}
              onSelect={() => handleSelect(option.value)}
              className="group flex cursor-pointer items-start gap-2.5 p-2"
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
          {shareUrl && canWrite && (
            <>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                onSelect={() => {
                  // Defer until the menu has closed, as with the private
                  // confirmation above.
                  setTimeout(() => setConfirmAction("reset"), 0);
                }}
                className="cursor-pointer gap-2 p-2"
              >
                <RotateCcw className="size-3.5" />
                Reset public link
              </DropdownMenuItem>
            </>
          )}
        </DropdownMenuContent>
      </DropdownMenu>

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
            <Button variant="secondary" onClick={() => setConfirmAction(null)}>
              Cancel
            </Button>
            <Button
              variant="destructive-primary"
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

/**
 * The public link under the skill's name, with a copy button. Renders nothing
 * while the skill is private.
 */
export function SkillPublicLink({
  skill,
}: {
  skill: Skill;
}): JSX.Element | null {
  const { domain: customDomain } = useCustomDomain(!!skill.shareToken);
  if (!skill.shareToken) return null;
  const shareUrl = skillShareUrl(
    skill.shareToken,
    skillShareDomain(customDomain),
  );
  return (
    <span className="text-muted-foreground flex h-6 min-w-0 items-center gap-1">
      <span
        className="max-w-[320px] truncate font-mono text-xs leading-6"
        title={shareUrl}
      >
        {shareUrl.replace(/^https?:\/\//, "")}
      </span>
      <CopyButton
        text={shareUrl}
        size="xs"
        tooltip="Copy public link"
        onCopy={() => {
          toast.success("Public link copied");
        }}
      />
    </span>
  );
}
