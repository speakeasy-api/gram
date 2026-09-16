import { useMemo, useState } from "react";
import {
  Check,
  ChevronDown,
  ExternalLink,
  KeyRound,
  Search,
} from "lucide-react";
import { useConfig as useMoonshineConfig } from "@/components/ui/hooks/useConfig";
import { useGenerateWorkOSAdminPortalLinkMutation } from "@gram/client/react-query/generateWorkOSAdminPortalLink.js";
import type { IdentityProviderConnection } from "@gram/client/models/components/identityproviderconnection.js";
import { useIdentityProvider } from "@gram/client/react-query/identityProvider.js";
import { useOnboardingStatus } from "@gram/client/react-query/onboardingStatus";
import { toast } from "sonner";
import { GuidedReadinessPanel } from "@/components/guided-readiness/guided-readiness-panel";
import { useGuidedReadiness } from "@/components/guided-readiness/use-guided-readiness";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Skeleton } from "@/components/ui/Skeleton";
import { openSafeExternalUrl } from "@/lib/safe-external-url";
import { cn, getServerURL } from "@/lib/utils";
import { StepContainer } from "../step-container";
import { StepSection } from "../step-section";
import { OktaApplicationsSection } from "./okta-applications-section";
import { OktaConnectSection } from "./okta-connect-section";
import { OktaDirectorySection } from "./okta-directory-section";
import { OktaSignOnSection } from "./okta-sign-on-section";
import { IDP_PROVIDERS } from "../../providers";
import type { IdpProvider } from "../../types";

const INITIAL_VISIBLE = 6;

/** The provider Speakeasy walks itself; see providers.ts. */
const GUIDED_PROVIDER_ID = "okta-oidc";

const DEFAULT_DESCRIPTION =
  "Pick the identity provider your organization already runs, then connect sign-in, mirror its directory, and carry the access those two give you into your MCP servers.";

const GUIDED_DESCRIPTION =
  "Connect Okta once. Speakeasy then configures single sign-on, reads your directory, and proposes MCP server access that matches the application assignments you already maintain in Okta.";

// The two outcomes no provider can reach yet. They are named and numbered from
// the start so the shape of the journey is the same whoever is walking it, and
// locked because nothing behind them exists to open.
/** Okta-only, and locked for every other provider until there is one. */
const APPLICATIONS_STEP = {
  index: 4,
  slug: "applications",
  title: "Applications and access",
  description:
    "Read what your identity provider assigns to each application and carry it into MCP server access as a reviewed proposal.",
  badge: "Waiting",
};

/**
 * The outcome no provider can reach yet. Named and numbered from the start so
 * the shape of the journey is the same whoever is walking it, and locked
 * because nothing behind it exists to open.
 */
const ENTERPRISE_STEP = {
  index: 5,
  slug: "enterprise-managed-auth",
  title: "Enterprise managed auth setup",
  // Not merely later in the queue: setup is complete without it, and it opens
  // on a capability Speakeasy does not have yet.
  description:
    "Setup finishes without this. It becomes available when Speakeasy can acquire credentials on a person's behalf, so people's agents stop signing in to each server separately.",
  badge: "Later",
};

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
  const { data: onboardingStatus, isLoading } = useOnboardingStatus(
    undefined,
    undefined,
    { throwOnError: false },
  );
  const [selectedProvider, setSelectedProvider] = useState<string | null>(null);
  const provider = IDP_PROVIDERS.find((p) => p.id === selectedProvider);
  const identityProvider = useIdentityProvider(undefined, undefined, {
    throwOnError: false,
  });
  const connection = identityProvider.data?.connection;
  const readiness = useGuidedReadiness(true);
  // A live connection is proof the pre-work was done, so it keeps the advanced
  // flow whatever the checks say about the organization now.
  const advancedOffered = !!connection || readiness.data?.eligible === true;
  // Until the check answers, the card behaves as though the advanced flow is
  // not offered. Offering it and then taking it back is worse than waiting.
  const readinessSettled = !readiness.isPending;
  // An explicit pick wins. With no pick, a connection that already exists is
  // what the card is about: coming back to this page must not read as though
  // nothing had been set up.
  const picked = provider ? provider.guided === true : !!connection;
  const guided = picked && advancedOffered;

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center">
          <KeyRound className="text-foreground h-6 w-6" />
        </div>
      }
      title="Set up identity provider"
      description={guided ? GUIDED_DESCRIPTION : DEFAULT_DESCRIPTION}
      onContinue={onComplete}
    >
      {/* The same five steps whoever the provider is. Picking one fills them
          in rather than replacing them: a provider Speakeasy walks itself
          takes over step 1 and waits on the rest, and one it does not hands
          steps 2 and 3 to the portal. */}
      <div className="space-y-8">
        {/* Staff only, and outside the numbered sections on purpose: it says
            why the card looks the way it does, whichever step is showing. */}
        <GuidedReadinessPanel />
        <SelectIdpSection
          index={1}
          selectedProvider={selectedProvider}
          onSelectProvider={setSelectedProvider}
          guided={guided}
          advancedOffered={advancedOffered}
          readinessSettled={readinessSettled}
          connection={connection}
          isLoadingConnection={identityProvider.isPending}
        />
        {guided ? (
          <OktaSignOnSection index={2} connection={connection} />
        ) : (
          <SingleSignOnSection
            index={2}
            configured={!!onboardingStatus?.ssoConfigured}
            isLoading={isLoading}
            provider={provider}
            locked={false}
          />
        )}
        {guided ? (
          <OktaDirectorySection index={3} connection={connection} />
        ) : (
          <DirectorySyncSection
            index={3}
            configured={!!onboardingStatus?.dsyncConfigured}
            isLoading={isLoading}
            locked={false}
          />
        )}
        {guided ? (
          <OktaApplicationsSection index={4} connection={connection} />
        ) : (
          <StepSection locked {...APPLICATIONS_STEP} />
        )}
        <StepSection locked {...ENTERPRISE_STEP} />
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
  /**
   * The provider Speakeasy walks itself owns this outcome, so the portal round
   * trip that normally sits here is not the way to reach it. The step keeps its
   * place and says what is coming instead.
   */
  locked: boolean;
}

// Step one for everybody: which identity provider this organization runs. A
// provider Speakeasy walks itself takes the rest of this step over, because
// connecting to it is the same decision continued rather than a new one.
function SelectIdpSection({
  index,
  selectedProvider,
  onSelectProvider,
  guided,
  advancedOffered,
  readinessSettled,
  connection,
  isLoadingConnection,
}: {
  index: number;
  selectedProvider: string | null;
  onSelectProvider: (id: string | null) => void;
  guided: boolean;
  /** Whether this organization can be taken down the advanced Okta flow. */
  advancedOffered: boolean;
  /** Whether the check behind that answer has come back yet. */
  readinessSettled: boolean;
  connection: IdentityProviderConnection | undefined;
  isLoadingConnection: boolean;
}): JSX.Element {
  const [showAll, setShowAll] = useState(false);
  const [query, setQuery] = useState("");

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

  // A connection outlives the click that made it, so after a reload the grid
  // still has to show which provider this organization is on.
  const shown = selectedProvider ?? (connection ? GUIDED_PROVIDER_ID : null);
  // Switching provider under a live connection would leave the connection
  // stranded, so the grid is a record of the choice until it is removed.
  const locked = !!connection;
  const complete = guided
    ? connection?.status === "active"
    : !!selectedProvider;

  return (
    <StepSection
      index={index}
      slug="select-idp"
      title="Select IDP"
      description="The identity provider your team already signs in with, and what it takes to connect it."
      complete={complete}
    >
      <div className="space-y-6">
        <div>
          <div className="relative">
            <Search className="text-muted-foreground pointer-events-none absolute top-[18px] left-3 h-4 w-4 -translate-y-1/2" />
            <Input
              type="search"
              value={query}
              onChange={setQuery}
              placeholder="Search providers"
              className="pl-9"
              disabled={locked}
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
                onClick={() => onSelectProvider(p.id)}
                disabled={locked}
                className={cn(
                  "flex items-center gap-3 border p-4 text-left transition-all",
                  shown === p.id
                    ? "border-foreground bg-secondary"
                    : "border-border bg-card hover:border-foreground/30",
                  locked && shown !== p.id && "opacity-50",
                  locked && "cursor-default",
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
                    {/* The guided entry keeps its place in the grid whatever
                        the checks say, but it only carries the badge where the
                        flow behind it is actually offered. */}
                    {p.badge && (!p.guided || advancedOffered) ? (
                      <Badge variant="success" background size="sm">
                        <Badge.Text>{p.badge}</Badge.Text>
                      </Badge>
                    ) : null}
                  </div>
                  <span className="text-muted-foreground block text-xs">
                    {p.protocol}
                  </span>
                  {/* Only the guided entry has a SAML sibling in this grid, and
                      the two read alike until you say which one to take. */}
                  {p.guided ? (
                    <span className="text-muted-foreground block text-xs">
                      Recommended over SAML
                    </span>
                  ) : null}
                </div>
              </button>
            ))}
          </div>
          {/* Said once, to the administrator who picked Okta and will be
              taken through the portal like everybody else. Why it is not
              offered is Speakeasy's business, not theirs. */}
          {shown === GUIDED_PROVIDER_ID &&
          readinessSettled &&
          !advancedOffered ? (
            <p className="text-muted-foreground mt-3 text-sm">
              Guided setup for Okta is not available for this organization yet.
            </p>
          ) : null}
          {!isSearching &&
            !showAll &&
            !locked &&
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

        {guided ? (
          <OktaConnectSection
            connection={connection}
            isLoadingConnection={isLoadingConnection}
          />
        ) : null}
      </div>
    </StepSection>
  );
}

function SingleSignOnSection({
  index,
  configured,
  isLoading,
  provider,
  locked,
}: SectionProps & { provider: IdpProvider | undefined }): JSX.Element {
  const [portalOpened, setPortalOpened] = useState(false);
  const [verifying, setVerifying] = useState(false);
  const { refetch: refetchOnboardingStatus } = useOnboardingStatus(
    undefined,
    undefined,
    { throwOnError: false },
  );

  const generatePortalLink = useGenerateWorkOSAdminPortalLinkMutation({
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to launch SSO setup portal",
      );
    },
  });

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
          "SSO connection not detected yet. Finish setup in the sign-in provider tab, then try again.",
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
        {provider && !isPending && (
          <PortalNote>
            {portalOpened
              ? `Finish configuring your ${provider.name} SSO connection in the sign-in provider tab, then verify it here.`
              : `After clicking Connect, the sign-in provider portal opens in a new browser tab to configure your ${provider.name} SSO connection. Finish setup there, then come back and verify.`}
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
      slug="single-sign-on"
      title="Single sign-on"
      description={
        locked
          ? "Speakeasy creates the sign-in application in Okta and proves it with a real sign-in."
          : "Let your team sign in with the credentials they already have."
      }
      complete={configured}
      locked={locked}
      badge={locked ? "Waiting" : undefined}
      aside={configured && !locked ? <ConnectedBadge /> : null}
    >
      {body}
    </StepSection>
  );
}

function DirectorySyncSection({
  index,
  configured,
  isLoading,
  locked,
}: SectionProps): JSX.Element {
  const [portalOpened, setPortalOpened] = useState(false);
  const [verifying, setVerifying] = useState(false);
  const { refetch: refetchOnboardingStatus } = useOnboardingStatus(
    undefined,
    undefined,
    { throwOnError: false },
  );

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
          "Directory sync not detected yet. Finish setup in the sign-in provider tab, then try again.",
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
            ? "Finish configuring the directory connection in the sign-in provider tab, then verify it here."
            : "After clicking Connect directory, the sign-in provider portal opens in a new browser tab. Finish configuring the connection there, then come back and verify."}
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
      slug="directory-sync"
      title="Directory sync"
      description="Keep users, groups, and roles in step with your identity provider automatically."
      complete={configured}
      locked={locked}
      badge={locked ? "Waiting" : undefined}
      aside={configured && !locked ? <ConnectedBadge /> : null}
    >
      {body}
    </StepSection>
  );
}
