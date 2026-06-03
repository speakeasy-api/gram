import { useState } from "react";
import { Book, ExternalLink, GitBranch, Loader2, Lock } from "lucide-react";
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
  const { data: publishStatus, isLoading } = usePublishStatus();

  const publishMutation = usePublishPluginsMutation({
    onSuccess: (data) => {
      setDialogOpen(false);
      invalidateAllPublishStatus(queryClient);
      toast.success("Plugins published to GitHub", {
        description: data.repoUrl,
      });
    },
    onError: () => {
      toast.error("Failed to publish plugins to GitHub");
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

  const primaryAction = isConnected ? onComplete : () => setDialogOpen(true);
  const primaryLabel = isConnected ? "Continue" : "Setup Plugin Marketplace";

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center rounded-lg">
          <GitBranch className="text-foreground h-6 w-6" />
        </div>
      }
      title="Create plugin marketplace"
      description="Gram publishes a private GitHub repo that acts as your team's plugin marketplace for Claude Code, Cursor, and Codex. It ships with the observability plugins out of the box and is also where any plugins you build in Gram later get published — so this only needs to be set up once per project."
      onContinue={primaryAction}
      continueLabel={primaryLabel}
      isLoading={publishMutation.isPending}
      canContinue={true}
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
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0 flex-1 space-y-2">
                <div className="flex flex-wrap items-center gap-2">
                  <Book className="text-muted-foreground h-4 w-4 flex-shrink-0" />
                  <span className="text-base">
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
                </div>
                <p className="text-muted-foreground text-sm leading-relaxed">
                  This repo is your team's plugin marketplace. The observability
                  plugins are already inside, and any plugins you build in Gram
                  later will be published here too. Continue to install them
                  into your agent platforms.
                </p>
                <div className="text-muted-foreground flex items-center gap-1.5 text-xs">
                  <Lock className="h-3 w-3" />
                  <span>Private</span>
                </div>
              </div>
              <a
                href={publishStatus.repoUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="border-border bg-background hover:bg-muted/50 inline-flex flex-shrink-0 items-center gap-1.5 rounded-md border px-3 py-1.5 text-sm font-medium transition-colors"
              >
                <ExternalLink className="h-3.5 w-3.5" />
                Open repo
              </a>
            </div>
          </div>
        ) : (
          <div className="bg-secondary/50 border-border rounded-lg border p-4">
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
      </div>

      <PublishDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        onPublish={handlePublish}
        isPending={publishMutation.isPending}
      />
    </StepContainer>
  );
}
