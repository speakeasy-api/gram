import { LogDataRetentionBanner } from "@/components/observe/LoggingPageHeader";
import { Button } from "@/components/ui/Button";
import { useOrganization } from "@/contexts/Auth";
import {
  ENABLE_LOGS_PAGE_DESCRIPTION,
  EnableLogsSetting,
} from "@/pages/org/EnableLogsSetting";
import { useOrgRoutes } from "@/routes";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { FileText } from "lucide-react";
import { Link } from "react-router";
import { StepContainer } from "../step-container";

interface EnableLoggingStepProps {
  onComplete: () => void;
  onSkip: () => void;
  onBack: () => void;
}

export function EnableLoggingStep({
  onComplete,
  onSkip,
  onBack,
}: EnableLoggingStepProps): JSX.Element {
  const organization = useOrganization();
  const orgRoutes = useOrgRoutes();
  const features = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false },
  );
  const logsEnabled = features.data?.logsEnabled === true;
  const featuresLoading = features.isLoading;
  const featuresFailed =
    !featuresLoading && Boolean(features.error || !features.data);

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center">
          <FileText className="text-foreground h-6 w-6" />
        </div>
      }
      title="Enable logs"
      description={ENABLE_LOGS_PAGE_DESCRIPTION}
      onContinue={onComplete}
      continueLabel="Continue"
      skipLabel="Skip for now"
      onSkip={logsEnabled ? undefined : onSkip}
      showBack
      onBack={onBack}
      canContinue={!featuresLoading && !featuresFailed}
      isLoading={featuresLoading}
    >
      <div className="space-y-6">
        <LogDataRetentionBanner />
        <div className="border-border bg-card border p-4">
          <EnableLogsSetting />
        </div>
        <p className="text-muted-foreground text-sm">
          This is the same Enable Logs setting as{" "}
          <Link
            to={orgRoutes.logs.href()}
            className="underline underline-offset-2"
          >
            Logging &amp; Telemetry
          </Link>
          . Tool I/O, session capture, and other options stay on that page.
        </p>
        {featuresFailed ? (
          <div
            className="border-border border p-4"
            role="alert"
            aria-live="polite"
          >
            <p className="text-destructive text-sm">
              Couldn&apos;t load the current logging setting.
            </p>
            <Button
              variant="secondary"
              size="sm"
              className="mt-3"
              disabled={features.isFetching}
              onClick={() => void features.refetch()}
            >
              {features.isFetching ? "Retrying…" : "Retry"}
            </Button>
          </div>
        ) : null}
      </div>
    </StepContainer>
  );
}
