import { useMemo, useState } from "react";
import {
  Check,
  ChevronDown,
  ExternalLink,
  Loader2,
  Search,
} from "lucide-react";
import { useConfig as useMoonshineConfig } from "@/components/ui/hooks/useConfig";
import { useGenerateWorkOSAdminPortalLinkMutation } from "@gram/client/react-query/generateWorkOSAdminPortalLink.js";
import { useOnboardingStatus } from "@gram/client/react-query/onboardingStatus";
import { toast } from "sonner";
import { Alert } from "@/components/ui/Alert";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Skeleton } from "@/components/ui/Skeleton";
import { openSafeExternalUrl } from "@/lib/safe-external-url";
import { cn, getServerURL } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import { setupTaskSlug } from "../../task-slugs";
import { StepContainer } from "../step-container";
import { StepSection } from "../step-section";
import { IDP_PROVIDERS } from "../../providers";
import type { IdpProvider } from "../../types";

const INITIAL_VISIBLE = 6;

interface IdentityProviderStepProps {
  onComplete: () => void;
}

// One card for the whole identity outcome: single sign-on and directory sync
// are two round trips through the WorkOS admin portal, but what the admin is
// setting up is "our identity provider runs sign-in and membership". Each
// sub-step carries its own inline action and flips to Connected from the
// server's onboarding status, so the card's Continue is always available.
export function IdentityProviderStep({
  onComplete,
}: IdentityProviderStepProps): JSX.Element {
  const {
    data: onboardingStatus,
    isLoading,
    isError,
    refetch,
  } = useOnboardingStatus(undefined, undefined, { throwOnError: false });

  // Only a status response can say the domain is unverified; while loading or
  // after an error, WorkOS still enforces the rule. An active SSO connection
  // proves a domain was verified, even for orgs set up before verified domains
  // were tracked — the server completes the domain setup task the same way.
  const domainVerified =
    onboardingStatus === undefined ||
    !!onboardingStatus.domainVerified ||
    !!onboardingStatus.ssoConfigured;

  return (
    <StepContainer
      title="Set up identity provider"
      description="Connect your SSO provider so your team signs in with existing credentials, then sync its directory so users, groups, and roles stay in step with your identity provider. Both can be finished later from organization settings."
      onContinue={onComplete}
    >
      <div className="space-y-8">
        {isError && <StatusUnavailableAlert onRetry={() => void refetch()} />}
        <SingleSignOnSection
          index={1}
          configured={!!onboardingStatus?.ssoConfigured}
          domainVerified={domainVerified}
          isLoading={isLoading}
        />
        <DirectorySyncSection
          index={2}
          configured={!!onboardingStatus?.dsyncConfigured}
          domainVerified={domainVerified}
          isLoading={isLoading}
        />
      </div>
    </StepContainer>
  );
}

/**
 * Both sub-steps read Connected off one onboarding status call, which reaches
 * WorkOS for the SSO connections and the directory. When that call fails the
 * card would otherwise render a confident "not connected" for setup that may
 * already exist, so say the state is unknown and offer the retry.
 */
function StatusUnavailableAlert({
  onRetry,
}: {
  onRetry: () => void;
}): JSX.Element {
  return (
    <Alert variant="warning" alignTop>
      <div className="text-sm">
        <p className="font-medium">Setup status unavailable</p>
        <p className="mt-1">
          We could not read your identity provider setup from WorkOS, so the
          states below may be out of date.
        </p>
        <Button
          variant="secondary"
          size="sm"
          onClick={onRetry}
          className="mt-3"
        >
          Try again
        </Button>
      </div>
    </Alert>
  );
}

type PortalIntent = "sso" | "dsync";

// Names the thing being set up in the middle of a sentence, so one message
// shape covers both portal round trips.
const PORTAL_NOUN: Record<PortalIntent, string> = {
  sso: "single sign-on",
  dsync: "directory sync",
};

const CONFIGURED_KEY: Record<
  PortalIntent,
  "ssoConfigured" | "dsyncConfigured"
> = {
  sso: "ssoConfigured",
  dsync: "dsyncConfigured",
};

/**
 * Drives one WorkOS admin portal round trip: mint the link, hand the admin a
 * tab, then re-check onboarding status when they come back. Every step reports
 * its own failure, because an admin looking at an unchanged card cannot tell a
 * refused portal link from one they have yet to finish — and a status check
 * that never completed is not the same as a connection WorkOS says is missing.
 */
function usePortalSetup(intent: PortalIntent) {
  const noun = PORTAL_NOUN[intent];
  const [portalOpened, setPortalOpened] = useState(false);
  const [verifying, setVerifying] = useState(false);
  const { refetch: refetchOnboardingStatus } = useOnboardingStatus(
    undefined,
    undefined,
    { throwOnError: false },
  );

  const generatePortalLink = useGenerateWorkOSAdminPortalLinkMutation({
    onError: (error) => {
      // The server explains the WorkOS failures it can classify; anything it
      // cannot still needs copy, or the click reads as a no-op.
      const detail = error instanceof Error ? error.message.trim() : "";
      toast.error(
        detail ||
          `Could not start ${noun} setup. Try again, and contact support if it keeps failing.`,
      );
    },
  });

  const connect = () => {
    generatePortalLink.mutate(
      {
        request: {
          generateWorkOSAdminPortalLinkRequestBody: {
            intent,
            successUrl: `${getServerURL()}/v1/setup/callback?intent=${intent}`,
            returnUrl: window.location.href,
            // NOTE: intent_options.sso.provider_type is intentionally omitted.
            // WorkOS currently only accepts "GoogleSAML" here and 422s on every
            // other provider, breaking non-Google onboarding. Omitting it lets
            // WorkOS open its own provider picker so all providers work. Restore
            // the selected provider type once WorkOS supports the full set:
            // https://speakeasyapi.slack.com/archives/C079KDQDY9X/p1781722173272439
          },
        },
      },
      {
        onSuccess: (data) => {
          if (openSafeExternalUrl(data.url)) {
            setPortalOpened(true);
            return;
          }
          toast.error(
            `Could not open the WorkOS portal for ${noun}. Allow pop-ups for this site, then try again.`,
          );
        },
      },
    );
  };

  const verify = async () => {
    setVerifying(true);
    try {
      const result = await refetchOnboardingStatus();
      if (result.data?.[CONFIGURED_KEY[intent]]) return;
      if (result.data === undefined) {
        toast.error(
          `Could not check the ${noun} connection. Try again in a moment.`,
        );
        return;
      }
      toast.error(
        `No ${noun} connection detected yet. Finish setup in the WorkOS tab, then try again.`,
      );
    } finally {
      setVerifying(false);
    }
  };

  return {
    connect,
    verify,
    verifying,
    portalOpened,
    isPending: generatePortalLink.isPending,
  };
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
  domainVerified: boolean;
  isLoading: boolean;
}

/** Points an admin at the step that unblocks the portal they just tried. */
function DomainRequiredNote(): JSX.Element {
  const orgRoutes = useOrgRoutes();

  return (
    <p className="text-muted-foreground text-sm">
      Verify a domain first.{" "}
      <orgRoutes.setupTask.Link
        params={[setupTaskSlug("domain-verification")]}
        className="text-foreground underline underline-offset-2"
      >
        Go to domain verification
      </orgRoutes.setupTask.Link>
    </p>
  );
}

function SingleSignOnSection({
  index,
  configured,
  domainVerified,
  isLoading,
}: SectionProps): JSX.Element {
  // WorkOS rejects a new SSO connection until the org has a verified domain.
  const needsDomain = !domainVerified && !configured;
  const [selectedProvider, setSelectedProvider] = useState<string | null>(null);
  const [showAll, setShowAll] = useState(false);
  const [query, setQuery] = useState("");
  const { connect, verify, verifying, portalOpened, isPending } =
    usePortalSetup("sso");

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

  const provider = IDP_PROVIDERS.find((p) => p.id === selectedProvider);

  const handleConnect = () => {
    if (!provider) return;
    connect();
  };

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

        {needsDomain && <DomainRequiredNote />}

        <div className="flex justify-end">
          {portalOpened ? (
            <Button
              variant="primary"
              size="sm"
              onClick={() => void verify()}
              disabled={verifying}
            >
              {verifying ? "Verifying..." : "Verify connection"}
            </Button>
          ) : (
            <Button
              variant="primary"
              size="sm"
              onClick={handleConnect}
              disabled={!provider || isPending || needsDomain}
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
      slug="single-sign-on"
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
  domainVerified,
  isLoading,
}: SectionProps): JSX.Element {
  // WorkOS needs a verified domain before a directory connection can be set
  // up, the same rule single sign-on is held to.
  const needsDomain = !domainVerified && !configured;
  const { connect, verify, verifying, portalOpened, isPending } =
    usePortalSetup("dsync");

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

        {needsDomain && <DomainRequiredNote />}

        <div className="flex justify-end">
          {portalOpened ? (
            <Button
              variant="primary"
              size="sm"
              onClick={() => void verify()}
              disabled={verifying}
            >
              {verifying ? "Verifying..." : "Verify connection"}
            </Button>
          ) : (
            <Button
              variant="primary"
              size="sm"
              onClick={connect}
              disabled={isPending || needsDomain}
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
      slug="directory-sync"
      title="Directory sync"
      description="Keep users, groups, and roles in step with your identity provider automatically."
      complete={configured}
      aside={configured ? <ConnectedBadge /> : null}
    >
      {body}
    </StepSection>
  );
}
