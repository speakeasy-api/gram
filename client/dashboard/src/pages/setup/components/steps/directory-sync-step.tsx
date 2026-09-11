import { useState } from "react";
import { Users, ExternalLink, Loader2 } from "lucide-react";
import { useGenerateWorkOSAdminPortalLinkMutation } from "@gram/client/react-query/generateWorkOSAdminPortalLink.js";
import { useOnboardingStatus } from "@gram/client/react-query/onboardingStatus";
import { toast } from "sonner";
import { StepContainer } from "../step-container";
import { openSafeExternalUrl } from "@/lib/safe-external-url";
import { getServerURL } from "@/lib/utils";

interface DirectorySyncStepProps {
  onComplete: () => void;
  onSkip: () => void;
  onBack: () => void;
}

export function DirectorySyncStep({
  onComplete,
}: DirectorySyncStepProps): JSX.Element {
  const [portalOpened, setPortalOpened] = useState(false);
  const [verifying, setVerifying] = useState(false);
  const {
    data: onboardingStatus,
    refetch: refetchOnboardingStatus,
    isLoading: statusLoading,
  } = useOnboardingStatus(undefined, undefined, { throwOnError: false });

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
            successUrl: `${getServerURL()}/v1/setup/callback?intent=dsync&task=directory-sync`,
            returnUrl: window.location.href,
          },
        },
      },
      {
        onSuccess: (data) => {
          if (openSafeExternalUrl(data.url)) {
            setPortalOpened(true);
          } else {
            toast.error(
              "Unable to open the WorkOS portal. Allow popups and try again.",
            );
          }
        },
      },
    );
  };

  const handleVerify = async () => {
    setVerifying(true);
    try {
      const result = await refetchOnboardingStatus();
      if (result.data?.dsyncConfigured) {
        onComplete();
      } else {
        toast.error(
          "Directory sync not detected yet. Finish setup in the WorkOS tab, then try again.",
        );
      }
    } finally {
      setVerifying(false);
    }
  };

  const connected = onboardingStatus?.dsyncConfigured === true;
  let continueAction = handleConnect;
  let continueLabel = "Connect directory";
  if (connected) {
    continueAction = onComplete;
    continueLabel = "Continue";
  } else if (portalOpened) {
    continueAction = () => void handleVerify();
    continueLabel = "Continue";
  }
  const isLoading = generatePortalLink.isPending || verifying || statusLoading;

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center">
          <Users className="text-foreground h-6 w-6" />
        </div>
      }
      title="Directory sync"
      description="Connect your identity provider's directory to automatically sync users, groups, and roles. Changes in your IdP will be reflected in Speakeasy."
      onContinue={continueAction}
      markDoneLabel={continueLabel}
      isLoading={isLoading}
    >
      <div className="space-y-6">
        {connected ? (
          <p role="status">Directory sync is connected.</p>
        ) : (
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
                  After clicking Connect directory, the WorkOS portal opens in a
                  new browser tab. Finish configuring the connection there, then
                  return here and click Continue.
                </p>
              </div>
            </div>
          </div>
        )}
        {generatePortalLink.isPending && (
          <div className="flex items-center justify-center py-8">
            <Loader2 className="text-muted-foreground h-6 w-6 animate-spin" />
          </div>
        )}
      </div>
    </StepContainer>
  );
}
