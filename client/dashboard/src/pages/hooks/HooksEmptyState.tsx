import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import { cn } from "@/lib/utils";
import { Icon } from "@speakeasy-api/moonshine";
import { Workflow } from "lucide-react";
import { useState } from "react";
import { HooksSetupDialog } from "./HooksSetupDialog";
import {
  ClaudeCodeIcon,
  CursorIcon,
  CodexIcon,
  CopilotIcon,
  GeminiIcon,
  GleanIcon,
  BedrockIcon,
} from "./HookSourceIcon";

interface ProviderCardProps {
  name: string;
  icon: React.ComponentType<{ className?: string }>;
  status: "available" | "coming-soon";
  onInstall: () => void;
}

function ProviderCard({
  name,
  icon: IconComponent,
  status,
  onInstall,
}: ProviderCardProps) {
  const isComingSoon = status === "coming-soon";

  return (
    <button
      onClick={onInstall}
      className={cn(
        "relative flex min-w-[160px] flex-col items-center rounded-lg border p-6 transition-all",
        status === "available"
          ? "border-border hover:border-primary hover:bg-muted/50 cursor-pointer"
          : "border-border/50 hover:border-primary/50 hover:bg-muted/30 cursor-pointer opacity-60",
      )}
    >
      <IconComponent className="mb-3 size-12" />
      <span className="text-sm font-medium">{name}</span>
      {isComingSoon && (
        <div className="absolute top-3 right-3">
          <span className="text-muted-foreground bg-muted rounded-full px-2 py-0.5 text-[10px] font-semibold tracking-wide uppercase">
            Coming Soon
          </span>
        </div>
      )}
    </button>
  );
}

export function HooksEmptyState() {
  const [showSetupDialog, setShowSetupDialog] = useState(false);
  const [setupProvider, setSetupProvider] = useState<"claude" | "cursor">(
    "claude",
  );
  const [showFeatureRequestModal, setShowFeatureRequestModal] = useState(false);
  const [selectedProvider, setSelectedProvider] = useState<string>("");

  const handleProviderClick = (provider: string, status: string) => {
    if (status === "coming-soon") {
      setSelectedProvider(provider);
      setShowFeatureRequestModal(true);
      return;
    }

    setSetupProvider(provider as "claude" | "cursor");
    setShowSetupDialog(true);
  };

  return (
    <>
      <div className="flex flex-col items-center justify-center px-4 py-16">
        <div className="w-full max-w-2xl space-y-8 text-center">
          {/* Icon and Title */}
          <div className="flex flex-col items-center gap-4">
            <div className="bg-muted flex size-16 items-center justify-center rounded-full">
              <Icon name="workflow" className="text-muted-foreground size-8" />
            </div>
            <div>
              <h2 className="mb-2 text-xl font-semibold">No Hook Logs Yet</h2>
              <p className="text-muted-foreground mx-auto max-w-md text-sm">
                Install Gram Hooks in your AI coding assistant to start
                capturing tool execution logs
              </p>
            </div>
          </div>

          {/* Installation Options */}
          <div>
            <h3 className="mb-4 text-sm font-medium">
              Choose Your AI Coding Assistant
            </h3>
            <div className="flex flex-wrap items-center justify-center gap-4">
              <ProviderCard
                name="Claude Code"
                icon={ClaudeCodeIcon}
                status="available"
                onInstall={() => handleProviderClick("claude", "available")}
              />
              <ProviderCard
                name="Cursor"
                icon={CursorIcon}
                status="available"
                onInstall={() => handleProviderClick("cursor", "available")}
              />
              <ProviderCard
                name="Codex"
                icon={CodexIcon}
                status="coming-soon"
                onInstall={() => handleProviderClick("codex", "coming-soon")}
              />
              <ProviderCard
                name="VSCode Copilot"
                icon={CopilotIcon}
                status="available"
                onInstall={() => handleProviderClick("copilot", "available")}
              />
              <ProviderCard
                name="Gemini"
                icon={GeminiIcon}
                status="coming-soon"
                onInstall={() => handleProviderClick("gemini", "coming-soon")}
              />
              <ProviderCard
                name="Glean"
                icon={GleanIcon}
                status="coming-soon"
                onInstall={() => handleProviderClick("glean", "coming-soon")}
              />
              <ProviderCard
                name="AWS Bedrock"
                icon={BedrockIcon}
                status="coming-soon"
                onInstall={() =>
                  handleProviderClick("aws-bedrock", "coming-soon")
                }
              />
            </div>
          </div>
        </div>
      </div>

      <HooksSetupDialog
        open={showSetupDialog}
        onOpenChange={setShowSetupDialog}
        defaultProvider={setupProvider}
      />

      {/* Feature Request Modal */}
      <FeatureRequestModal
        isOpen={showFeatureRequestModal}
        onClose={() => setShowFeatureRequestModal(false)}
        title={`${selectedProvider.charAt(0).toUpperCase() + selectedProvider.slice(1)} Integration`}
        description={`Support for ${selectedProvider.charAt(0).toUpperCase() + selectedProvider.slice(1)} is coming soon. Let us know you're interested and we'll notify you when it's available.`}
        actionType={`hooks_${selectedProvider}_integration`}
        icon={Workflow}
        telemetryData={{ provider: selectedProvider }}
      />
    </>
  );
}
