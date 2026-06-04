import { useState } from "react";
import { Book, ExternalLink, GitBranch, Loader2, Users } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  invalidateAllPublishStatus,
  usePublishStatus,
} from "@gram/client/react-query/publishStatus";
import { usePublishPluginsMutation } from "@gram/client/react-query/publishPlugins";
import { StepContainer } from "../step-container";
import { PublishDialog } from "@/pages/plugins/PublishDialog";

interface CreateMarketplaceStepProps {
  onComplete: () => void;
  onBack: () => void;
}

export function CreateMarketplaceStep({
  onComplete,
  onBack,
}: CreateMarketplaceStepProps) {
  const queryClient = useQueryClient();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [dialogMode, setDialogMode] = useState<"publish" | "manage">("publish");
  const { data: publishStatus, isLoading } = usePublishStatus();

  const publishMutation = usePublishPluginsMutation({
    onSuccess: (data) => {
      setDialogOpen(false);
      invalidateAllPublishStatus(queryClient);
      toast.success(
        dialogMode === "manage"
          ? "Collaborators added"
          : "Plugins published to GitHub",
        { description: data.repoUrl },
      );
    },
    onError: () => {
      toast.error(
        dialogMode === "manage"
          ? "Failed to add collaborators"
          : "Failed to publish plugins to GitHub",
      );
    },
  });

  const handlePublish = (githubUsernames: string[]) => {
    publishMutation.mutate({
      security: { sessionHeaderGramSession: "" },
      request: {
        publishPluginsRequestBody: { githubUsernames },
      },
    });
  };

  const isConnected = !!(publishStatus?.connected && publishStatus.repoUrl);

  const openPublishDialog = () => {
    setDialogMode("publish");
    setDialogOpen(true);
  };
  const openManageDialog = () => {
    setDialogMode("manage");
    setDialogOpen(true);
  };

  const primaryAction = isConnected ? onComplete : openPublishDialog;
  const primaryLabel = isConnected ? "Continue" : "Setup Plugin Marketplace";

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center rounded-lg">
          <GitBranch className="text-foreground h-6 w-6" />
        </div>
      }
      title="Create plugin marketplace"
      description="Gram publishes a private GitHub repo that acts as your team's plugin marketplace for Claude Code, Cursor, and Codex. It ships with our core observability plugin, required for us to collect usage metrics and enforce authorization, and is also where any plugins you build in Gram later get published — so this only needs to be set up once per project."
      onContinue={primaryAction}
      continueLabel={primaryLabel}
      isLoading={publishMutation.isPending || isLoading}
      canContinue={!isLoading}
      showBack
      onBack={onBack}
    >
      <div className="space-y-4">
        {isLoading ? (
          <div className="flex items-center justify-center py-8">
            <Loader2 className="text-muted-foreground h-6 w-6 animate-spin" />
          </div>
        ) : isConnected ? (
          <div className="bg-card border-border rounded-md border p-4">
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              <Book className="text-muted-foreground h-4 w-4 flex-shrink-0" />
              <span className="min-w-0 truncate text-base">
                {publishStatus.repoOwner && (
                  <a
                    href={publishStatus.repoUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="text-sky-500 hover:text-sky-600 hover:underline"
                  >
                    {publishStatus.repoOwner}
                  </a>
                )}
                {publishStatus.repoOwner && publishStatus.repoName && (
                  <span className="text-muted-foreground/60 mx-1">/</span>
                )}
                {publishStatus.repoName && (
                  <a
                    href={publishStatus.repoUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="font-semibold text-sky-500 hover:text-sky-600 hover:underline"
                  >
                    {publishStatus.repoName}
                  </a>
                )}
              </span>
              <span className="border-border text-muted-foreground rounded-full border px-2 py-0 text-[10px] font-medium tracking-wider uppercase">
                Private
              </span>
            </div>
            <p className="text-muted-foreground mt-2 text-sm leading-relaxed">
              This repo is your team's plugin marketplace. The observability
              plugins are already inside, and any plugins you build in Gram
              later will be published here too.
            </p>
            <div className="mt-3 flex flex-wrap items-center justify-between gap-3">
              <span className="text-muted-foreground inline-flex items-center gap-1.5 text-xs">
                <span className="relative flex h-2 w-2">
                  <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-500 opacity-40" />
                  <span className="relative inline-flex h-2 w-2 rounded-full bg-emerald-500" />
                </span>
                <span className="font-medium text-emerald-700 dark:text-emerald-400">
                  Marketplace set up
                </span>
              </span>
              <div className="flex flex-wrap items-center gap-2">
                <a
                  href={publishStatus.repoUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="border-border bg-background hover:bg-muted/50 inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs font-medium transition-colors"
                >
                  <ExternalLink className="h-3 w-3" />
                  Open
                </a>
                <button
                  type="button"
                  onClick={openManageDialog}
                  className="border-border bg-background hover:bg-muted/50 inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1 text-xs font-medium transition-colors"
                >
                  <Users className="h-3 w-3" />
                  Manage collaborators
                </button>
              </div>
            </div>
          </div>
        ) : (
          <div className="bg-card border-border rounded-lg border p-4">
            <div className="flex items-start gap-3">
              <div className="bg-secondary mt-0.5 flex h-8 w-8 flex-shrink-0 items-center justify-center rounded">
                <GitBranch className="text-muted-foreground h-4 w-4" />
              </div>
              <div>
                <p className="text-foreground text-sm font-medium">
                  Publish a private GitHub repo for your team
                </p>
                <p className="text-muted-foreground mt-1 text-sm">
                  Clicking the button below opens a dialog where you can
                  optionally add GitHub usernames who get read access to the
                  repo. At least one user needs access so they can connect the
                  marketplace to Claude, Cursor, or Codex on their machine.
                </p>
              </div>
            </div>
          </div>
        )}
        {isConnected && (
          <p className="text-muted-foreground text-sm leading-relaxed">
            At least one of your organization's users needs to be attached to
            the repository as a collaborator so that the repository is
            discoverable by Agent Platforms.
          </p>
        )}
      </div>

      <PublishDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        onPublish={handlePublish}
        isPending={publishMutation.isPending}
        mode={dialogMode}
      />
    </StepContainer>
  );
}
