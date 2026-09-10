import { useState, type ReactNode } from "react";
import { Book, ExternalLink, GitBranch, Users } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import type { PublishStatusResult } from "@gram/client/models/components/publishstatusresult.js";
import {
  invalidateAllPublishStatus,
  usePublishStatus,
} from "@gram/client/react-query/publishStatus";
import { usePublishPluginsMutation } from "@gram/client/react-query/publishPlugins";
import { PublishDialog } from "@/pages/plugins/PublishDialog";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { StepSection } from "./step-section";
import { isMarketplacePublished } from "./marketplace-status";

type DialogMode = "publish" | "manage";

interface MarketplaceSectionProps {
  index: number;
  /**
   * Hold the step open until the repo has collaborators. Claude.ai syncs the
   * marketplace repo through its own GitHub App and cannot read one nobody
   * has been given access to, so the Anthropic card needs sharing done before
   * the step means anything. The other cards only need the repo to exist.
   */
  requiresCollaborators?: boolean;
  /** Why this card needs the marketplace, shown under the section title. */
  description: string;
  /** What to do with the published repo in this card's flow, if anything. */
  publishedHint?: string;
}

// Publishing the plugin marketplace is a mechanical prerequisite rather than
// an outcome of its own, so it drops into the front of whichever card needs
// it (Anthropic observability, other platforms, Distribute MCP servers) instead
// of being a card. It only needs doing once per project; every later card
// just sees the published state.
export function MarketplaceSection({
  index,
  requiresCollaborators = false,
  description,
  publishedHint,
}: MarketplaceSectionProps): JSX.Element {
  const queryClient = useQueryClient();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [dialogMode, setDialogMode] = useState<DialogMode>("publish");
  // throwOnError: false so a failed read degrades to "not published" and the
  // card keeps its publish prompt. The shared QueryClient only suppresses
  // 401/403, so anything else would replace the whole setup page with an error
  // screen — the guard the wizard used to carry before its cards took over.
  const { data: publishStatus, isLoading } = usePublishStatus(
    undefined,
    undefined,
    { throwOnError: false },
  );
  const published = isMarketplacePublished(publishStatus);
  // Only a definite "no" holds the step open. getPublishStatus omits the flag
  // when the collaborator lookup itself failed, and its comment there is
  // explicit that the dashboard must read a missing value as unknown rather
  // than false — a transient GitHub error must not dead-end setup.
  const noCollaborators = publishStatus?.hasCollaborators === false;

  const publishMutation = usePublishPluginsMutation({
    onSuccess: (data) => {
      setDialogOpen(false);
      void invalidateAllPublishStatus(queryClient);
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

  const openDialog = (mode: DialogMode) => {
    setDialogMode(mode);
    setDialogOpen(true);
  };

  let body: ReactNode;
  if (isLoading) {
    body = (
      <Skeleton>
        <div className="h-[74px] w-full" />
      </Skeleton>
    );
  } else if (published && publishStatus) {
    body = (
      <PublishedRepoRow
        publishStatus={publishStatus}
        hint={publishedHint}
        onManageCollaborators={() => openDialog("manage")}
      />
    );
  } else {
    body = <PublishPrompt onPublish={() => openDialog("publish")} />;
  }

  return (
    <StepSection
      index={index}
      slug="publish-marketplace"
      title="Publish plugin marketplace"
      description={description}
      complete={published && !(requiresCollaborators && noCollaborators)}
      aside={
        published ? (
          <Badge variant="success" background>
            <Badge.Text>Published</Badge.Text>
          </Badge>
        ) : null
      }
    >
      {body}
      <PublishDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        onPublish={(githubUsernames) =>
          publishMutation.mutate({
            security: { sessionHeaderGramSession: "" },
            request: { publishPluginsRequestBody: { githubUsernames } },
          })
        }
        isPending={publishMutation.isPending}
        mode={dialogMode}
      />
    </StepSection>
  );
}

function PublishPrompt({ onPublish }: { onPublish: () => void }): JSX.Element {
  return (
    <div className="border-border bg-card flex flex-col gap-4 border p-4 sm:flex-row sm:items-center">
      <div className="bg-secondary flex h-10 w-10 flex-shrink-0 items-center justify-center">
        <GitBranch className="text-muted-foreground h-5 w-5" />
      </div>
      <div className="min-w-0 flex-1">
        <p className="text-foreground text-sm font-medium">
          Publish a private GitHub repo for your team
        </p>
        <p className="text-muted-foreground mt-1 text-sm leading-relaxed">
          Speakeasy publishes a private repo that acts as your plugin
          marketplace. It ships with the observability plugin and receives any
          plugins you build later, so this only happens once per project. You
          can add GitHub usernames who get read access.
        </p>
      </div>
      <Button variant="primary" size="sm" onClick={onPublish}>
        Publish marketplace
      </Button>
    </div>
  );
}

function PublishedRepoRow({
  publishStatus,
  hint,
  onManageCollaborators,
}: {
  publishStatus: PublishStatusResult;
  hint?: string;
  onManageCollaborators: () => void;
}): JSX.Element {
  return (
    <div className="border-border bg-card flex flex-col gap-3 border p-4 sm:flex-row sm:items-center">
      <div className="bg-secondary flex h-10 w-10 flex-shrink-0 items-center justify-center">
        <Book className="text-muted-foreground h-5 w-5" />
      </div>
      <div className="min-w-0 flex-1">
        <a
          href={publishStatus.repoUrl}
          target="_blank"
          rel="noopener noreferrer"
          className="text-foreground inline-flex max-w-full items-center gap-1 text-sm font-medium underline underline-offset-2"
        >
          <span className="truncate">
            {publishStatus.repoOwner}/{publishStatus.repoName}
          </span>
          <ExternalLink className="h-3 w-3 flex-shrink-0" />
        </a>
        <p className="text-muted-foreground mt-1 text-xs leading-relaxed">
          {hint ? `${hint} ` : ""}At least one GitHub collaborator needs access
          so the repo is discoverable from your team's agents.
        </p>
      </div>
      <Button variant="secondary" size="sm" onClick={onManageCollaborators}>
        <Users className="h-4 w-4" />
        Manage collaborators
      </Button>
    </div>
  );
}
