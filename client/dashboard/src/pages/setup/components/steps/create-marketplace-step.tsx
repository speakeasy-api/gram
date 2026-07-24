import { useState } from "react";
import { GitBranch, Loader2 } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  invalidateAllPublishStatus,
  usePublishStatus,
} from "@gram/client/react-query/publishStatus";
import { usePublishPluginsMutation } from "@gram/client/react-query/publishPlugins";
import { StepContainer } from "../step-container";
import { MarketplaceCard } from "@/pages/plugins/MarketplaceCard";
import { PublishDialog } from "@/pages/plugins/PublishDialog";

interface CreateMarketplaceStepProps {
  onComplete: () => void;
  onBack: () => void;
}

export function CreateMarketplaceStep({
  onComplete,
  onBack,
}: CreateMarketplaceStepProps): JSX.Element {
  const queryClient = useQueryClient();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [dialogMode, setDialogMode] = useState<"publish" | "manage">("publish");
  const { data: publishStatus, isLoading } = usePublishStatus();

  const publishMutation = usePublishPluginsMutation({
    onSuccess: (data) => {
      setDialogOpen(false);
      void invalidateAllPublishStatus(queryClient, { refetchType: "all" });
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
      description="Speakeasy publishes a private GitHub repo that acts as your team's plugin marketplace for Claude Code, Cursor, and Codex. It ships with our core observability plugin, required for us to collect usage metrics and enforce authorization, and is also where any plugins you build in Speakeasy later get published — so this only needs to be set up once per project."
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
          <MarketplaceCard
            publishStatus={publishStatus}
            onManageCollaborators={openManageDialog}
          />
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
