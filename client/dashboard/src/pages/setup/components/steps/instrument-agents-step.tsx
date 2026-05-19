import { useState } from "react";
import { Terminal, Check, Copy, ExternalLink } from "lucide-react";
import { StepContainer } from "../step-container";
import { AGENT_PLATFORMS } from "../../mock-data";
import type { AgentPlatform } from "../../types";
import { Switch } from "@/components/ui/switch";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

interface InstrumentAgentsStepProps {
  onComplete: () => void;
  onBack: () => void;
}

const MOCK_WEBHOOK_URL = "https://api.speakeasy.com/hooks/acme-corp/agents";
const MOCK_API_KEY = "sk_live_speakeasy_xxxxxxxxxxxxx";

export function InstrumentAgentsStep({
  onComplete,
  onBack,
}: InstrumentAgentsStepProps) {
  const [platforms, setPlatforms] = useState<AgentPlatform[]>(AGENT_PLATFORMS);
  const [copiedField, setCopiedField] = useState<string | null>(null);

  const connectedCount = platforms.filter((p) => p.connected).length;

  const togglePlatform = (platformId: string) => {
    setPlatforms((prev) =>
      prev.map((p) =>
        p.id === platformId ? { ...p, connected: !p.connected } : p,
      ),
    );
  };

  const copyToClipboard = async (text: string, field: string) => {
    await navigator.clipboard.writeText(text);
    setCopiedField(field);
    setTimeout(() => setCopiedField(null), 2000);
  };

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center rounded-lg">
          <Terminal className="text-foreground h-6 w-6" />
        </div>
      }
      title="Instrument agent platforms"
      description="Connect the AI coding assistants used by your team. This enables traffic monitoring and policy enforcement."
      onContinue={onComplete}
      continueLabel="Continue"
      showBack
      onBack={onBack}
    >
      <div className="space-y-6">
        {/* Credentials */}
        <div className="border-border bg-card rounded-lg border p-4">
          <label className="text-foreground text-sm font-medium">
            Integration credentials
          </label>
          <p className="text-muted-foreground mt-1 mb-4 text-sm">
            Use these credentials to configure your agent platforms.
          </p>
          <div className="space-y-3">
            <div>
              <label className="text-muted-foreground text-xs">
                Webhook URL
              </label>
              <div className="mt-1 flex items-center gap-2">
                <code className="bg-secondary text-foreground flex-1 truncate rounded px-3 py-2 font-mono text-sm">
                  {MOCK_WEBHOOK_URL}
                </code>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => copyToClipboard(MOCK_WEBHOOK_URL, "webhook")}
                  className="flex-shrink-0"
                >
                  {copiedField === "webhook" ? (
                    <Check className="text-success h-4 w-4" />
                  ) : (
                    <Copy className="h-4 w-4" />
                  )}
                </Button>
              </div>
            </div>
            <div>
              <label className="text-muted-foreground text-xs">API Key</label>
              <div className="mt-1 flex items-center gap-2">
                <code className="bg-secondary text-foreground flex-1 truncate rounded px-3 py-2 font-mono text-sm">
                  {MOCK_API_KEY}
                </code>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => copyToClipboard(MOCK_API_KEY, "apikey")}
                  className="flex-shrink-0"
                >
                  {copiedField === "apikey" ? (
                    <Check className="text-success h-4 w-4" />
                  ) : (
                    <Copy className="h-4 w-4" />
                  )}
                </Button>
              </div>
            </div>
          </div>
        </div>

        {/* Platforms */}
        <div>
          <div className="mb-3 flex items-center justify-between">
            <label className="text-muted-foreground text-sm font-medium tracking-wide uppercase">
              Agent platforms
            </label>
            <span className="text-muted-foreground text-xs">
              {connectedCount} of {platforms.length} enabled
            </span>
          </div>
          <div className="space-y-2">
            {platforms.map((platform) => (
              <div
                key={platform.id}
                className={cn(
                  "flex items-center gap-4 rounded-lg border p-4 transition-colors",
                  platform.connected
                    ? "border-foreground/20 bg-secondary/50"
                    : "border-border bg-card",
                )}
              >
                <div
                  className={cn(
                    "flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg",
                    platform.connected ? "bg-foreground/10" : "bg-secondary",
                  )}
                >
                  <span className="text-foreground text-base font-semibold">
                    {platform.name.charAt(0)}
                  </span>
                </div>
                <div className="min-w-0 flex-1">
                  <p className="text-foreground text-sm font-medium">
                    {platform.name}
                  </p>
                  <p className="text-muted-foreground truncate text-xs">
                    {platform.description}
                  </p>
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  className="text-muted-foreground flex-shrink-0"
                >
                  <ExternalLink className="mr-1 h-3 w-3" />
                  Docs
                </Button>
                <Switch
                  checked={platform.connected}
                  onCheckedChange={() => togglePlatform(platform.id)}
                />
              </div>
            ))}
          </div>
        </div>
      </div>
    </StepContainer>
  );
}
