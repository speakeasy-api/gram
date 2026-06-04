import { useMemo, useState } from "react";
import {
  KeyRound,
  ExternalLink,
  Loader2,
  ChevronDown,
  Search,
} from "lucide-react";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import { useGenerateWorkOSAdminPortalLinkMutation } from "@gram/client/react-query";
import { useOnboardingStatus } from "@gram/client/react-query/onboardingStatus";
import { toast } from "sonner";
import { StepContainer } from "../step-container";
import { IDP_PROVIDERS } from "../../providers";
import type { IdpProvider } from "../../types";
import { Input } from "@/components/ui/input";
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
  onSkip: () => void;
  onComplete: () => void;
}

const INITIAL_VISIBLE = 6;

export function ConnectIdpStep({ onSkip, onComplete }: ConnectIdpStepProps) {
  const [selectedProvider, setSelectedProvider] = useState<string | null>(null);
  const [showAll, setShowAll] = useState(false);
  const [query, setQuery] = useState("");
  const [portalOpened, setPortalOpened] = useState(false);
  const [verifying, setVerifying] = useState(false);
  const { refetch: refetchOnboardingStatus } = useOnboardingStatus();

  const filteredProviders = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return IDP_PROVIDERS;
    return IDP_PROVIDERS.filter(
      (p) =>
        p.name.toLowerCase().includes(q) ||
        p.protocol.toLowerCase().includes(q),
    );
  }, [query]);

  const isSearching = query.trim().length > 0;
  const visibleProviders = isSearching
    ? filteredProviders
    : showAll
      ? IDP_PROVIDERS
      : IDP_PROVIDERS.slice(0, INITIAL_VISIBLE);

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
            returnUrl: window.location.href,
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
          window.open(data.url, "_blank", "noopener,noreferrer");
          setPortalOpened(true);
        },
      },
    );
  };

  const handleVerify = async () => {
    setVerifying(true);
    try {
      const result = await refetchOnboardingStatus();
      if (result.data?.ssoConfigured) {
        onComplete();
      } else {
        toast.error(
          "SSO connection not detected yet. Finish setup in the WorkOS tab, then try again.",
        );
      }
    } finally {
      setVerifying(false);
    }
  };

  const continueAction = portalOpened ? handleVerify : handleConnect;
  const continueLabel = portalOpened
    ? verifying
      ? "Verifying..."
      : "Continue"
    : generatePortalLink.isPending
      ? "Opening..."
      : "Connect";
  const isLoading = generatePortalLink.isPending || verifying;

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center rounded-lg">
          <KeyRound className="text-foreground h-6 w-6" />
        </div>
      }
      title="Connect identity provider"
      description="Connect your SSO provider to enable secure authentication for your team. This allows employees to sign in with their existing credentials."
      onContinue={continueAction}
      onSkip={onSkip}
      skipLabel="Skip for now"
      continueLabel={continueLabel}
      isLoading={isLoading}
      canContinue={!!selectedProvider}
    >
      <div className="space-y-6">
        <div>
          <label className="text-foreground text-sm font-medium">
            Select provider<span className="text-accent">*</span>
          </label>
          <div className="relative mt-3">
            <Search className="text-muted-foreground pointer-events-none absolute top-[18px] left-3 h-4 w-4 -translate-y-1/2" />
            <Input
              type="search"
              value={query}
              onChange={setQuery}
              placeholder="Search providers"
              className="pl-9"
              disabled={generatePortalLink.isPending}
            />
          </div>
          {isSearching && filteredProviders.length === 0 && (
            <p className="text-muted-foreground mt-3 text-sm">
              No providers match &quot;{query}&quot;.
            </p>
          )}
          <div className="mt-3 grid grid-cols-2 gap-3">
            {visibleProviders.map((p) => (
              <button
                key={p.id}
                onClick={() => setSelectedProvider(p.id)}
                disabled={generatePortalLink.isPending}
                className={cn(
                  "flex items-center gap-3 rounded-lg border p-4 text-left transition-all",
                  selectedProvider === p.id
                    ? "border-foreground bg-secondary"
                    : "border-border bg-card hover:border-foreground/30",
                  generatePortalLink.isPending &&
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
          {!isSearching &&
            !showAll &&
            IDP_PROVIDERS.length > INITIAL_VISIBLE && (
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

        {selectedProvider && !generatePortalLink.isPending && (
          <div className="bg-card border-border rounded-lg border p-4">
            <div className="flex items-start gap-3">
              <div className="bg-secondary mt-0.5 flex h-8 w-8 flex-shrink-0 items-center justify-center rounded">
                <ExternalLink className="text-muted-foreground h-4 w-4" />
              </div>
              <div>
                <p className="text-foreground text-sm font-medium">
                  Setup opens in a new tab
                </p>
                <p className="text-muted-foreground mt-1 text-sm">
                  After clicking Connect, the WorkOS portal opens in a new
                  browser tab to configure your {provider?.name} SSO connection.
                  Finish setup there, then return here and click Continue.
                </p>
              </div>
            </div>
          </div>
        )}
      </div>
    </StepContainer>
  );
}
