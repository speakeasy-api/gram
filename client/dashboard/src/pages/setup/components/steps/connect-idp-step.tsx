import { useState } from "react";
import {
  KeyRound,
  Check,
  ExternalLink,
  Loader2,
  ChevronDown,
} from "lucide-react";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import { useGenerateWorkOSAdminPortalLinkMutation } from "@gram/client/react-query";
import { toast } from "sonner";
import { StepContainer } from "../step-container";
import { IDP_PROVIDERS } from "../../providers";
import type { IdpProvider } from "../../types";
import { cn, getServerURL } from "@/lib/utils";

function ProviderIcon({
  provider,
  className,
}: {
  provider: IdpProvider;
  className?: string;
}) {
  const { theme } = useMoonshineConfig();
  const variant = theme === "dark" ? "dark" : "light";
  return (
    <img
      src={`https://cdn.workos.com/provider-icons/${variant}/${provider.iconSlug}.svg`}
      alt={provider.name}
      className={cn("h-6 w-6", className)}
    />
  );
}

interface ConnectIdpStepProps {
  onComplete: () => void;
}

const INITIAL_VISIBLE = 6;

export function ConnectIdpStep({ onComplete }: ConnectIdpStepProps) {
  const [selectedProvider, setSelectedProvider] = useState<string | null>(null);
  const [isConnected, setIsConnected] = useState(false);
  const [showAll, setShowAll] = useState(false);

  const generatePortalLink = useGenerateWorkOSAdminPortalLinkMutation({
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to launch SSO setup portal",
      );
    },
  });

  const provider = IDP_PROVIDERS.find((p) => p.id === selectedProvider);

  const handleConnect = () => {
    if (!provider) return;

    generatePortalLink.mutate(
      {
        request: {
          generateWorkOSAdminPortalLinkRequestBody: {
            intent: "sso",
            successUrl: `${getServerURL()}/v1/setup/callback?intent=sso`,
            intentOptions: {
              sso: {
                providerType: provider.providerType,
              },
            },
          },
        },
      },
      {
        onSuccess: (data) => {
          window.location.href = data.url;
        },
      },
    );
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
        isConnected
          ? "Continue"
          : generatePortalLink.isPending
            ? "Opening..."
            : "Connect"
      }
      isLoading={generatePortalLink.isPending}
      canContinue={!!selectedProvider}
    >
      <div className="space-y-6">
        <div>
          <label className="text-foreground text-sm font-medium">
            Select provider<span className="text-accent">*</span>
          </label>
          <div className="mt-3 grid grid-cols-2 gap-3">
            {(showAll
              ? IDP_PROVIDERS
              : IDP_PROVIDERS.slice(0, INITIAL_VISIBLE)
            ).map((p) => (
              <button
                key={p.id}
                onClick={() => {
                  if (!isConnected) {
                    setSelectedProvider(p.id);
                  }
                }}
                disabled={isConnected || generatePortalLink.isPending}
                className={cn(
                  "flex items-center gap-3 rounded-lg border p-4 text-left transition-all",
                  selectedProvider === p.id
                    ? "border-foreground bg-secondary"
                    : "border-border bg-card hover:border-foreground/30",
                  (isConnected || generatePortalLink.isPending) &&
                    selectedProvider !== p.id &&
                    "cursor-not-allowed opacity-50",
                )}
              >
                <div className="bg-secondary flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg">
                  <ProviderIcon provider={p} />
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="text-foreground truncate text-sm font-medium">
                      {p.name}
                    </span>
                    {selectedProvider === p.id && isConnected && (
                      <span className="text-success bg-success/10 flex items-center gap-1 rounded px-1.5 py-0.5 text-xs">
                        <Check className="h-3 w-3" />
                        Connected
                      </span>
                    )}
                    {selectedProvider === p.id &&
                      generatePortalLink.isPending && (
                        <Loader2 className="text-muted-foreground h-3.5 w-3.5 animate-spin" />
                      )}
                  </div>
                  <span className="text-muted-foreground text-xs">
                    {p.protocol}
                  </span>
                </div>
              </button>
            ))}
          </div>
          {!showAll && IDP_PROVIDERS.length > INITIAL_VISIBLE && (
            <button
              type="button"
              onClick={() => setShowAll(true)}
              className="text-muted-foreground hover:text-foreground mt-2 flex w-full items-center justify-center gap-1.5 py-2 text-sm transition-colors"
            >
              <ChevronDown className="h-4 w-4" />
              Show {IDP_PROVIDERS.length - INITIAL_VISIBLE} more providers
            </button>
          )}
        </div>

        {selectedProvider && !isConnected && !generatePortalLink.isPending && (
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
                  After clicking Connect, you will be redirected to configure
                  your {provider?.name} SSO connection. Once complete, you will
                  be returned to the next step automatically.
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
                  Setup portal opened
                </p>
                <p className="text-muted-foreground mt-1 text-sm">
                  Complete the configuration in the WorkOS portal window. Once
                  done, click Continue to proceed.
                </p>
              </div>
            </div>
          </div>
        )}
      </div>
    </StepContainer>
  );
}
