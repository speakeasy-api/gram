import { useMemo, useState } from "react";
import {
  Check,
  ChevronDown,
  ExternalLink,
  KeyRound,
  Loader2,
  Search,
} from "lucide-react";
import { useConfig as useMoonshineConfig } from "@/components/ui/hooks/useConfig";
import { useGenerateWorkOSAdminPortalLinkMutation } from "@gram/client/react-query/generateWorkOSAdminPortalLink.js";
import { useOnboardingStatus } from "@gram/client/react-query/onboardingStatus";
import { toast } from "sonner";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Skeleton } from "@/components/ui/Skeleton";
import { openSafeExternalUrl } from "@/lib/safe-external-url";
import { cn, getServerURL } from "@/lib/utils";
import { StepContainer } from "../step-container";
import { StepSection } from "../step-section";
import { IDP_PROVIDERS } from "../../providers";
import type { IdpProvider } from "../../types";

const INITIAL_VISIBLE = 6;

interface IdentityProviderStepProps {
  onComplete: () => void;
  onBack?: () => void;
}

// One card for the whole identity outcome: single sign-on and directory sync
// are two round trips through the WorkOS admin portal, but what the admin is
// setting up is "our identity provider runs sign-in and membership". Each
// sub-step carries its own inline action and flips to Connected from the
// server's onboarding status, so the card's Continue is always available.
export function IdentityProviderStep({
  onComplete,
  onBack,
}: IdentityProviderStepProps): JSX.Element {
  const { data: onboardingStatus, isLoading } = useOnboardingStatus();

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center">
          <KeyRound className="text-foreground h-6 w-6" />
        </div>
      }
      title="Set up identity provider"
      description="Connect your SSO provider so your team signs in with existing credentials, then sync its directory so users, groups, and roles stay in step with your identity provider. Both can be finished later from organization settings."
      onContinue={onComplete}
      continueLabel="Continue"
      showBack={onBack !== undefined}
      onBack={onBack}
    >
      <div className="space-y-8">
        <SingleSignOnSection
          index={1}
          configured={!!onboardingStatus?.ssoConfigured}
          isLoading={isLoading}
        />
        <DirectorySyncSection
          index={2}
          configured={!!onboardingStatus?.dsyncConfigured}
          isLoading={isLoading}
        />
      </div>
    </StepContainer>
  );
}

function ConnectedBadge(): JSX.Element {
  return (
    <Badge variant="success" background>
      <Badge.LeftIcon>
        <Check className="h-3 w-3" />
      </Badge.LeftIcon>
      <Badge.Text>Connected</Badge.Text>
    </Badge>
  );
}

function ConnectedNote({
  title,
  children,
}: {
  title: string;
  children: string;
}): JSX.Element {
  return (
    <div className="border-border bg-card border p-4">
      <p className="text-foreground text-sm font-medium">{title}</p>
      <p className="text-muted-foreground mt-1 text-sm">{children}</p>
    </div>
  );
}

function PortalNote({ children }: { children: string }): JSX.Element {
  return (
    <div className="bg-card border-border border p-4">
      <div className="flex items-start gap-3">
        <div className="bg-secondary mt-0.5 flex h-8 w-8 flex-shrink-0 items-center justify-center">
          <ExternalLink className="text-muted-foreground h-4 w-4" />
        </div>
        <div>
          <p className="text-foreground text-sm font-medium">
            Setup opens in a new tab
          </p>
          <p className="text-muted-foreground mt-1 text-sm">{children}</p>
        </div>
      </div>
    </div>
  );
}

function SectionSkeleton(): JSX.Element {
  return (
    <Skeleton>
      <div className="h-[74px] w-full" />
    </Skeleton>
  );
}

function ProviderIcon({
  provider,
  className,
}: {
  provider: IdpProvider;
  className?: string;
}): JSX.Element {
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

interface SectionProps {
  index: number;
  configured: boolean;
  isLoading: boolean;
}

function SingleSignOnSection({
  index,
  configured,
  isLoading,
}: SectionProps): JSX.Element {
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
  let visibleProviders = IDP_PROVIDERS.slice(0, INITIAL_VISIBLE);
  if (isSearching) visibleProviders = filteredProviders;
  else if (showAll) visibleProviders = IDP_PROVIDERS;

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
            // NOTE: intent_options.sso.provider_type is intentionally omitted.
            // WorkOS currently only accepts "GoogleSAML" here and 422s on every
            // other provider, breaking non-Google onboarding. Omitting it lets
            // WorkOS open its own provider picker so all providers work. Restore
            // provider.providerType once WorkOS supports the full set:
            // https://speakeasyapi.slack.com/archives/C079KDQDY9X/p1781722173272439
          },
        },
      },
      {
        onSuccess: (data) => {
          if (openSafeExternalUrl(data.url)) setPortalOpened(true);
        },
      },
    );
  };

  // Verifying refetches the shared onboarding status, so a successful check
  // flips this section to Connected through the parent's query data.
  const handleVerify = async () => {
    setVerifying(true);
    try {
      const result = await refetchOnboardingStatus();
      if (!result.data?.ssoConfigured) {
        toast.error(
          "SSO connection not detected yet. Finish setup in the WorkOS tab, then try again.",
        );
      }
    } finally {
      setVerifying(false);
    }
  };

  const isPending = generatePortalLink.isPending;

  let body: JSX.Element;
  if (isLoading) {
    body = <SectionSkeleton />;
  } else if (configured) {
    body = (
      <ConnectedNote title="Single sign-on is connected">
        Your team signs in through your identity provider. Manage the connection
        from organization settings.
      </ConnectedNote>
    );
  } else {
    body = (
      <div className="space-y-4">
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
              disabled={isPending}
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
                type="button"
                onClick={() => setSelectedProvider(p.id)}
                disabled={isPending}
                className={cn(
                  "flex items-center gap-3 border p-4 text-left transition-all",
                  selectedProvider === p.id
                    ? "border-foreground bg-secondary"
                    : "border-border bg-card hover:border-foreground/30",
                  isPending &&
                    selectedProvider !== p.id &&
                    "cursor-not-allowed opacity-50",
                )}
              >
                <div className="bg-secondary flex h-10 w-10 flex-shrink-0 items-center justify-center">
                  <ProviderIcon provider={p} />
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="text-foreground truncate text-sm font-medium">
                      {p.name}
                    </span>
                    {selectedProvider === p.id && isPending && (
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

        {provider && !isPending && (
          <PortalNote>
            {portalOpened
              ? `Finish configuring your ${provider.name} SSO connection in the WorkOS tab, then verify it here.`
              : `After clicking Connect, the WorkOS portal opens in a new browser tab to configure your ${provider.name} SSO connection. Finish setup there, then come back and verify.`}
          </PortalNote>
        )}

        <div className="flex justify-end">
          {portalOpened ? (
            <Button
              variant="primary"
              size="sm"
              onClick={() => void handleVerify()}
              disabled={verifying}
            >
              {verifying ? "Verifying..." : "Verify connection"}
            </Button>
          ) : (
            <Button
              variant="primary"
              size="sm"
              onClick={handleConnect}
              disabled={!provider || isPending}
            >
              {isPending ? "Opening..." : "Connect"}
            </Button>
          )}
        </div>
      </div>
    );
  }

  return (
    <StepSection
      index={index}
      title="Single sign-on"
      description="Let your team sign in with the credentials they already have."
      complete={configured}
      aside={configured ? <ConnectedBadge /> : null}
    >
      {body}
    </StepSection>
  );
}

function DirectorySyncSection({
  index,
  configured,
  isLoading,
}: SectionProps): JSX.Element {
  const [portalOpened, setPortalOpened] = useState(false);
  const [verifying, setVerifying] = useState(false);
  const { refetch: refetchOnboardingStatus } = useOnboardingStatus();

  const generatePortalLink = useGenerateWorkOSAdminPortalLinkMutation({
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to launch directory sync portal",
      );
    },
  });

  const handleConnect = () => {
    generatePortalLink.mutate(
      {
        request: {
          generateWorkOSAdminPortalLinkRequestBody: {
            intent: "dsync",
            successUrl: `${getServerURL()}/v1/setup/callback?intent=dsync`,
            returnUrl: window.location.href,
          },
        },
      },
      {
        onSuccess: (data) => {
          if (openSafeExternalUrl(data.url)) setPortalOpened(true);
        },
      },
    );
  };

  const handleVerify = async () => {
    setVerifying(true);
    try {
      const result = await refetchOnboardingStatus();
      if (!result.data?.dsyncConfigured) {
        toast.error(
          "Directory sync not detected yet. Finish setup in the WorkOS tab, then try again.",
        );
      }
    } finally {
      setVerifying(false);
    }
  };

  const isPending = generatePortalLink.isPending;

  let body: JSX.Element;
  if (isLoading) {
    body = <SectionSkeleton />;
  } else if (configured) {
    body = (
      <ConnectedNote title="Directory sync is connected">
        Users, groups, and roles now follow your identity provider. Manage the
        connection from organization settings.
      </ConnectedNote>
    );
  } else {
    body = (
      <div className="space-y-4">
        <PortalNote>
          {portalOpened
            ? "Finish configuring the directory connection in the WorkOS tab, then verify it here."
            : "After clicking Connect directory, the WorkOS portal opens in a new browser tab. Finish configuring the connection there, then come back and verify."}
        </PortalNote>
        <div className="flex justify-end">
          {portalOpened ? (
            <Button
              variant="primary"
              size="sm"
              onClick={() => void handleVerify()}
              disabled={verifying}
            >
              {verifying ? "Verifying..." : "Verify connection"}
            </Button>
          ) : (
            <Button
              variant="primary"
              size="sm"
              onClick={handleConnect}
              disabled={isPending}
            >
              {isPending ? "Opening..." : "Connect directory"}
            </Button>
          )}
        </div>
      </div>
    );
  }

  return (
    <StepSection
      index={index}
      title="Directory sync"
      description="Keep users, groups, and roles in step with your identity provider automatically."
      complete={configured}
      aside={configured ? <ConnectedBadge /> : null}
    >
      {body}
    </StepSection>
  );
}
