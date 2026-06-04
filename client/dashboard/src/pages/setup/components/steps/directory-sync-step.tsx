import { Users, ExternalLink, Loader2 } from "lucide-react";
import { useGenerateWorkOSAdminPortalLinkMutation } from "@gram/client/react-query";
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

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center rounded-lg">
          <Users className="text-foreground h-6 w-6" />
        </div>
      }
      title="Directory sync"
      description="Connect your identity provider's directory to automatically sync users, groups, and roles. Changes in your IdP will be reflected in Gram."
      onContinue={handleConnect}
      continueLabel={
        generatePortalLink.isPending ? "Opening..." : "Connect directory"
      }
      isLoading={generatePortalLink.isPending}
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
                {"You'll"} be redirected to complete setup
              </p>
              <p className="text-muted-foreground mt-1 text-sm">
                After clicking Connect directory, you will be redirected to
                configure your directory sync connection. Once complete, you
                will be returned to the next step automatically.
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
