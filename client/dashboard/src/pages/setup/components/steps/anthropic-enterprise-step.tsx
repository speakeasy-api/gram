import { useState } from "react";
import { Book, ExternalLink } from "lucide-react";
import { usePublishStatus } from "@gram/client/react-query/publishStatus";
import type { PublishStatusResult } from "@gram/client/models/components/publishstatusresult.js";
import { AGENT_PROVIDERS } from "@/components/agent-providers/agent-providers";
import { AgentProviderIcon } from "@/components/agent-providers/AgentProviderIcon";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Skeleton } from "@/components/ui/Skeleton";
import { StepContainer } from "../step-container";
import { AgentPlatformPickerItem } from "../agent-platform-picker-item";
import { PlatformInstrumentationSheet } from "../platform-instrumentation-sheet";
import { platformStatusBadge } from "../platform-status-badge";
import { ANTHROPIC_ENTERPRISE_PLATFORM_ID } from "../../setup-data";
import type { PlatformSetupStatus } from "../../types";

interface AnthropicEnterpriseStepProps {
  onComplete: () => void;
  onBack: () => void;
}

export function AnthropicEnterpriseStep({
  onComplete,
  onBack,
}: AnthropicEnterpriseStepProps): JSX.Element {
  const [sheetOpen, setSheetOpen] = useState(false);
  const [status, setStatus] = useState<PlatformSetupStatus>("not_started");
  const { data: publishStatus, isLoading } = usePublishStatus();
  const provider = AGENT_PROVIDERS[ANTHROPIC_ENTERPRISE_PLATFORM_ID];

  // Claude.ai syncs Cowork plugins straight from the marketplace's GitHub
  // repo, and the instructions quote its owner/name — so without a published
  // marketplace there is nothing to register yet.
  const isConnected = !!(publishStatus?.connected && publishStatus.repoUrl);

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center">
          <AgentProviderIcon source="claude" className="h-6 w-6" />
        </div>
      }
      title="Set up Anthropic Enterprise"
      description="Claude Cowork runs in Claude.ai's cloud sandbox, out of the device agent's reach, so it is configured from Claude.ai's organization settings instead. Register your plugin marketplace there and require the observability plugin so every Cowork session reports to Speakeasy. Claude Code is instrumented alongside your other coding assistants under Instrument agents."
      onContinue={onComplete}
      continueLabel="Continue"
      showBack
      onBack={onBack}
    >
      <div className="space-y-3">
        {isLoading ? (
          <Skeleton>
            <div className="h-[74px] w-full" />
          </Skeleton>
        ) : isConnected ? (
          <MarketplaceRepoRow publishStatus={publishStatus} />
        ) : (
          <Alert variant="warning">
            <AlertTitle>Publish your plugin marketplace first</AlertTitle>
            <AlertDescription>
              Claude.ai syncs Cowork plugins straight from your marketplace's
              GitHub repo, so it has to exist before you can register it. Go
              back to Create plugin marketplace to publish it.
            </AlertDescription>
          </Alert>
        )}

        <AgentPlatformPickerItem
          platformId={ANTHROPIC_ENTERPRISE_PLATFORM_ID}
          name={provider.name}
          description={provider.description}
          complete={status === "complete"}
          statusBadge={platformStatusBadge(status)}
          disabled={!isConnected}
          onClick={() => setSheetOpen(true)}
        />
      </div>

      <PlatformInstrumentationSheet
        open={sheetOpen}
        onOpenChange={setSheetOpen}
        initialPlatformId={ANTHROPIC_ENTERPRISE_PLATFORM_ID}
        onPlatformStatusChange={(_, next) => setStatus(next)}
      />
    </StepContainer>
  );
}

// The repo the Cowork instructions ask the admin to pick inside Claude.ai,
// shown up front so they know what they're looking for before opening them.
function MarketplaceRepoRow({
  publishStatus,
}: {
  publishStatus: PublishStatusResult;
}): JSX.Element {
  return (
    <div className="border-border bg-card flex items-center gap-4 border p-4">
      <div className="bg-secondary flex h-10 w-10 flex-shrink-0 items-center justify-center">
        <Book className="text-muted-foreground h-5 w-5" />
      </div>
      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex items-center gap-2">
          <p className="text-foreground text-sm font-medium">
            Plugin marketplace
          </p>
          <Badge variant="success" background>
            <Badge.Text>Published</Badge.Text>
          </Badge>
        </div>
        <p className="text-muted-foreground text-xs">
          Claude.ai will sync plugins from{" "}
          <a
            href={publishStatus.repoUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="text-foreground inline-flex items-center gap-1 underline underline-offset-2"
          >
            {publishStatus.repoOwner}/{publishStatus.repoName}
            <ExternalLink className="h-3 w-3" />
          </a>
          . Select that repo when Claude.ai asks.
        </p>
      </div>
    </div>
  );
}
