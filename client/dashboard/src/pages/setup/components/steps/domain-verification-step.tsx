import { useState } from "react";
import { Check, ExternalLink } from "lucide-react";
import { useGenerateWorkOSAdminPortalLinkMutation } from "@gram/client/react-query/generateWorkOSAdminPortalLink.js";
import { useOnboardingStatus } from "@gram/client/react-query/onboardingStatus";
import { toast } from "sonner";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Skeleton } from "@/components/ui/Skeleton";
import { openSafeExternalUrl } from "@/lib/safe-external-url";
import { getServerURL } from "@/lib/utils";
import { StepContainer } from "../step-container";
import { StepSection } from "../step-section";

interface DomainVerificationStepProps {
  onComplete: () => void;
}

// WorkOS only lets an organization set up single sign-on once it has proved
// it owns an email domain, so this task comes before the identity provider.
// The domain is verified in the WorkOS admin portal (a DNS record), and the
// section flips to Verified from the server's onboarding status.
export function DomainVerificationStep({
  onComplete,
}: DomainVerificationStepProps): JSX.Element {
  const {
    data: onboardingStatus,
    isLoading,
    refetch,
  } = useOnboardingStatus(undefined, undefined, { throwOnError: false });
  // Active SSO proves a domain was verified, even for orgs set up before
  // verified domains were tracked. The server completes the task the same way.
  const verified =
    !!onboardingStatus?.domainVerified || !!onboardingStatus?.ssoConfigured;
  const verifiedDomains = onboardingStatus?.verifiedDomains ?? [];
  const [portalOpened, setPortalOpened] = useState(false);
  const [verifying, setVerifying] = useState(false);

  const generatePortalLink = useGenerateWorkOSAdminPortalLinkMutation({
    onError: (error) => {
      toast.error(
        error instanceof Error
          ? error.message
          : "Failed to launch domain verification portal",
      );
    },
  });

  const handleConnect = () => {
    generatePortalLink.mutate(
      {
        request: {
          generateWorkOSAdminPortalLinkRequestBody: {
            intent: "domain_verification",
            successUrl: `${getServerURL()}/v1/setup/callback?intent=domain_verification`,
            returnUrl: window.location.href,
          },
        },
      },
      {
        onSuccess: (data) => {
          if (openSafeExternalUrl(data.url)) {
            setPortalOpened(true);
          } else {
            toast.error("Unable to open the WorkOS portal");
          }
        },
      },
    );
  };

  const handleVerify = async () => {
    setVerifying(true);
    try {
      const result = await refetch();
      if (!result.data?.domainVerified) {
        toast.error(
          "Domain not verified yet. Finish setup in the WorkOS tab, then try again.",
        );
      }
    } finally {
      setVerifying(false);
    }
  };

  const isPending = generatePortalLink.isPending;

  let body: JSX.Element;
  if (isLoading) {
    body = (
      <Skeleton>
        <div className="h-[74px] w-full" />
      </Skeleton>
    );
  } else if (verified) {
    body = (
      <div className="border-border bg-card border p-4">
        <p className="text-foreground text-sm font-medium">
          Your domain is verified
        </p>
        <p className="text-muted-foreground mt-1 text-sm">
          You can now connect your identity provider for single sign-on.
        </p>
        {verifiedDomains.length > 0 && (
          <>
            <p className="text-muted-foreground mt-3 text-sm">
              SSO applies to users on these domains:
            </p>
            <ul
              aria-label="Verified domains"
              className="mt-2 flex flex-wrap gap-2"
            >
              {verifiedDomains.map((domain) => (
                <li key={domain}>
                  <Badge variant="neutral" background>
                    <Badge.Text>{domain}</Badge.Text>
                  </Badge>
                </li>
              ))}
            </ul>
          </>
        )}
      </div>
    );
  } else {
    body = (
      <div className="space-y-4">
        <div className="bg-card border-border border p-4">
          <div className="flex items-start gap-3">
            <div className="bg-secondary mt-0.5 flex h-8 w-8 flex-shrink-0 items-center justify-center">
              <ExternalLink className="text-muted-foreground h-4 w-4" />
            </div>
            <div>
              <p className="text-foreground text-sm font-medium">
                Setup opens in a new tab
              </p>
              <p className="text-muted-foreground mt-1 text-sm">
                {portalOpened
                  ? "Add the DNS record shown in the WorkOS tab, then verify it here."
                  : "After clicking Verify domain, the WorkOS portal opens in a new browser tab. Add the DNS record it shows, then come back and verify."}
              </p>
            </div>
          </div>
        </div>
        <div className="flex justify-end">
          {portalOpened ? (
            <Button
              variant="primary"
              size="sm"
              onClick={() => void handleVerify()}
              disabled={verifying}
            >
              {verifying ? "Verifying..." : "Check verification"}
            </Button>
          ) : (
            <Button
              variant="primary"
              size="sm"
              onClick={handleConnect}
              disabled={isPending}
            >
              {isPending ? "Opening..." : "Verify domain"}
            </Button>
          )}
        </div>
      </div>
    );
  }

  return (
    <StepContainer
      title="Verify your domain"
      description="Prove your organization owns its email domain. Single sign-on needs a verified domain before you can connect an identity provider."
      onContinue={onComplete}
    >
      <StepSection
        index={1}
        slug="verify-domain"
        title="Verify domain"
        description="Add a DNS record to prove you own your email domain."
        complete={verified}
        aside={
          verified ? (
            <Badge variant="success" background>
              <Badge.LeftIcon>
                <Check className="h-3 w-3" />
              </Badge.LeftIcon>
              <Badge.Text>Verified</Badge.Text>
            </Badge>
          ) : null
        }
      >
        {body}
      </StepSection>
    </StepContainer>
  );
}
