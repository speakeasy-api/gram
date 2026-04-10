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
        "relative flex flex-col items-center p-6 rounded-lg border transition-all min-w-[160px]",
        status === "available"
          ? "border-border hover:border-primary hover:bg-muted/50 cursor-pointer"
          : "border-border/50 hover:border-primary/50 hover:bg-muted/30 cursor-pointer opacity-60",
      )}
    >
      <IconComponent className="size-12 mb-3" />
      <span className="font-medium text-sm">{name}</span>
      {isComingSoon && (
        <div className="absolute top-3 right-3">
          <span className="text-[10px] font-semibold text-muted-foreground bg-muted px-2 py-0.5 rounded-full uppercase tracking-wide">
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
      <div className="flex flex-col items-center justify-center py-16 px-4">
        <div className="max-w-2xl w-full text-center space-y-8">
          {/* Icon and Title */}
          <div className="flex flex-col items-center gap-4">
            <div className="size-16 rounded-full bg-muted flex items-center justify-center">
              <Icon name="workflow" className="size-8 text-muted-foreground" />
            </div>
            <div>
              <h2 className="text-xl font-semibold mb-2">No Hook Logs Yet</h2>
              <p className="text-sm text-muted-foreground max-w-md mx-auto">
                Install Gram Hooks in your AI coding assistant to start
                capturing tool execution logs
              </p>
            </div>
          </div>

          {/* Installation Options */}
          <div>
            <h3 className="text-sm font-medium mb-4">
              Choose Your AI Coding Assistant
            </h3>
            <div className="flex items-center justify-center gap-4 flex-wrap">
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
                name="Copilot"
                icon={CopilotIcon}
                status="coming-soon"
                onInstall={() => handleProviderClick("copilot", "coming-soon")}
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
