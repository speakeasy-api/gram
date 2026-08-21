import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import { Icon } from "@/components/ui/Icon";
import { Workflow } from "lucide-react";
import { useState } from "react";
import { HooksSetupDialog } from "./HooksSetupDialog";
import {
  ClaudeCodeIcon,
  CursorIcon,
  CodexIcon,
} from "@/components/agent-providers/AgentProviderIcon";

interface ProviderCardProps {
  name: string;
  icon: React.ComponentType<{ className?: string }>;
  onInstall: () => void;
}

function ProviderCard({
  name,
  icon: IconComponent,
  onInstall,
}: ProviderCardProps) {
  return (
    <button
      type="button"
      onClick={onInstall}
      className="border-border hover:border-primary hover:bg-muted/50 relative flex min-w-[160px] cursor-pointer flex-col items-center border p-6 transition-all"
    >
      <IconComponent className="mb-3 size-12" />
      <span className="text-sm font-medium">{name}</span>
    </button>
  );
}

export function HooksEmptyState({
  title = "No logs captured",
  subtitle = "Install Observability plugin in your AI agent to start capturing tool execution logs",
}: {
  title?: string;
  subtitle?: string;
} = {}): JSX.Element {
  const [showSetupDialog, setShowSetupDialog] = useState(false);
  const [setupProvider, setSetupProvider] = useState<
    "claude" | "cursor" | "codex"
  >("claude");
  const [showFeatureRequestModal, setShowFeatureRequestModal] = useState(false);

  const handleProviderClick = (provider: "claude" | "cursor" | "codex") => {
    setSetupProvider(provider);
    setShowSetupDialog(true);
  };

  return (
    <>
      <div className="flex flex-col items-center justify-center px-4 py-16">
        <div className="w-full max-w-2xl space-y-8 text-center">
          {/* Icon and Title */}
          <div className="flex flex-col items-center gap-4">
            <div className="border-border flex size-16 items-center justify-center border">
              <Icon name="workflow" className="text-muted-foreground size-8" />
            </div>
            <div>
              <h2 className="text-display-sm mb-2 font-thin">{title}</h2>
              <p className="text-muted-foreground mx-auto max-w-md text-sm">
                {subtitle}
              </p>
            </div>
          </div>

          {/* Installation Options */}
          <div>
            <h3 className="text-eyebrow mb-4">
              Choose Your AI Coding Assistant
            </h3>
            <div className="flex flex-wrap items-center justify-center gap-4">
              <ProviderCard
                name="Claude Code"
                icon={ClaudeCodeIcon}
                onInstall={() => handleProviderClick("claude")}
              />
              <ProviderCard
                name="Cursor"
                icon={CursorIcon}
                onInstall={() => handleProviderClick("cursor")}
              />
              <ProviderCard
                name="Codex"
                icon={CodexIcon}
                onInstall={() => handleProviderClick("codex")}
              />
            </div>
            <button
              type="button"
              className="text-muted-foreground hover:text-foreground mt-4 text-sm underline underline-offset-4"
              onClick={() => setShowFeatureRequestModal(true)}
            >
              Don&apos;t see your agent? Request an integration
            </button>
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
        title="Request an Observability Integration"
        description="Tell us which AI agent your team uses. We'll use your feedback to prioritize new integrations."
        actionType="hooks_agent_integration"
        icon={Workflow}
        requestInput={{
          label: "AI agent",
          placeholder: "e.g. GitHub Copilot",
          telemetryField: "requested_agent",
        }}
      />
    </>
  );
}
