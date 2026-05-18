import { useState } from "react";
import { KeyRound, Check, ExternalLink } from "lucide-react";
import { StepContainer } from "../step-container";
import { IDP_PROVIDERS } from "../../mock-data";
import { cn } from "@/lib/utils";

interface ConnectIdpStepProps {
  onComplete: () => void;
}

export function ConnectIdpStep({ onComplete }: ConnectIdpStepProps) {
  const [selectedProvider, setSelectedProvider] = useState<string | null>(null);
  const [isConnecting, setIsConnecting] = useState(false);
  const [isConnected, setIsConnected] = useState(false);

  const handleConnect = async () => {
    if (!selectedProvider) return;

    setIsConnecting(true);
    // Simulate connection
    await new Promise((resolve) => setTimeout(resolve, 1500));
    setIsConnecting(false);
    setIsConnected(true);
  };

  const handleContinue = () => {
    if (isConnected) {
      onComplete();
    } else {
      handleConnect();
    }
  };

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center rounded-lg">
          <KeyRound className="text-foreground h-6 w-6" />
        </div>
      }
      title="Connect identity provider"
      description="Connect your SSO provider to enable secure authentication for your team. This allows employees to sign in with their existing credentials."
      onContinue={handleContinue}
      continueLabel={
        isConnected ? "Continue" : isConnecting ? "Connecting..." : "Connect"
      }
      isLoading={isConnecting}
      canContinue={!!selectedProvider}
    >
      <div className="space-y-6">
        <div>
          <label className="text-foreground text-sm font-medium">
            Select provider<span className="text-accent">*</span>
          </label>
          <div className="mt-3 grid grid-cols-2 gap-3">
            {IDP_PROVIDERS.map((provider) => (
              <button
                key={provider.id}
                onClick={() => {
                  if (!isConnected) {
                    setSelectedProvider(provider.id);
                  }
                }}
                disabled={isConnected}
                className={cn(
                  "flex items-center gap-3 rounded-lg border p-4 text-left transition-all",
                  selectedProvider === provider.id
                    ? "border-foreground bg-secondary"
                    : "border-border bg-card hover:border-foreground/30",
                  isConnected &&
                    selectedProvider !== provider.id &&
                    "cursor-not-allowed opacity-50",
                )}
              >
                <div className="bg-secondary flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg">
                  <span className="text-lg">{provider.icon}</span>
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="text-foreground truncate text-sm font-medium">
                      {provider.name}
                    </span>
                    {selectedProvider === provider.id && isConnected && (
                      <span className="text-success bg-success/10 flex items-center gap-1 rounded px-1.5 py-0.5 text-xs">
                        <Check className="h-3 w-3" />
                        Connected
                      </span>
                    )}
                  </div>
                  <span className="text-muted-foreground text-xs">
                    {provider.type}
                  </span>
                </div>
              </button>
            ))}
          </div>
        </div>

        {selectedProvider && !isConnected && (
          <div className="bg-secondary/50 border-border rounded-lg border p-4">
            <div className="flex items-start gap-3">
              <div className="bg-secondary mt-0.5 flex h-8 w-8 flex-shrink-0 items-center justify-center rounded">
                <ExternalLink className="text-muted-foreground h-4 w-4" />
              </div>
              <div>
                <p className="text-foreground text-sm font-medium">
                  {"You'll"} be redirected to complete setup
                </p>
                <p className="text-muted-foreground mt-1 text-sm">
                  After clicking Connect, {"you'll"} be taken to your identity
                  provider to authorize the connection.
                </p>
              </div>
            </div>
          </div>
        )}

        {isConnected && (
          <div className="bg-success/5 border-success/20 rounded-lg border p-4">
            <div className="flex items-start gap-3">
              <div className="bg-success/10 mt-0.5 flex h-8 w-8 flex-shrink-0 items-center justify-center rounded">
                <Check className="text-success h-4 w-4" />
              </div>
              <div>
                <p className="text-foreground text-sm font-medium">
                  Successfully connected
                </p>
                <p className="text-muted-foreground mt-1 text-sm">
                  Your identity provider is now linked. Directory sync will
                  begin automatically.
                </p>
              </div>
            </div>
          </div>
        )}
      </div>
    </StepContainer>
  );
}
