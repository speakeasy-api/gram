import { useState } from "react";
import { Users, ExternalLink, Loader2 } from "lucide-react";
import { useGenerateWorkOSAdminPortalLinkMutation } from "@gram/client/react-query";
import { useOnboardingStatus } from "@gram/client/react-query/onboardingStatus";
import { toast } from "sonner";
import { StepContainer } from "../step-container";
import { getServerURL } from "@/lib/utils";

interface DirectorySyncStepProps {
  onComplete: () => void;
  onBack: () => void;
}

export function DirectorySyncStep({
  onComplete,
  onBack,
}: DirectorySyncStepProps) {
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

  const continueAction = portalOpened ? handleVerify : handleConnect;
  const continueLabel = portalOpened
    ? verifying
      ? "Verifying..."
      : "Continue"
    : generatePortalLink.isPending
      ? "Opening..."
      : "Connect directory";
  const isLoading = generatePortalLink.isPending || verifying;

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center rounded-lg">
          <Users className="text-foreground h-6 w-6" />
        </div>
      }
      title="Directory sync"
      description="Connect your identity provider's directory to automatically sync users, groups, and roles. Changes in your IdP will be reflected in Gram."
      onContinue={continueAction}
      continueLabel={continueLabel}
      isLoading={isLoading}
      showBack
      onBack={onBack}
      onSkip={onComplete}
      skipLabel="Skip for now"
    >
      <div className="space-y-6">
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
                After clicking Connect directory, the WorkOS portal opens in a
                new browser tab. Finish configuring the connection there, then
                return here and click Continue.
              </p>
            </div>
          </div>
        </div>

        {generatePortalLink.isPending && (
          <div className="flex items-center justify-center py-8">
            <Loader2 className="text-muted-foreground h-6 w-6 animate-spin" />
          </div>
        )}
      </div>
    </StepContainer>
  );
}
