import { LogDataRetentionBanner } from "@/components/observe/LoggingPageHeader";
import { Button } from "@/components/ui/Button";
import { useOrganization } from "@/contexts/Auth";
import { ENABLE_LOGS_PAGE_DESCRIPTION } from "@/pages/org/EnableLogsSetting";
import { useOrgRoutes } from "@/routes";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { FileText } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router";
import { EnableLoggingAndSessionCaptureSetting } from "../enable-logging-and-session-capture-setting";
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
  const [bundleEnabled, setBundleEnabled] = useState<boolean | null>(null);
  const [bundleBusy, setBundleBusy] = useState(false);
  const loggingBundleEnabled =
    bundleEnabled ??
    (features.data?.logsEnabled === true &&
      features.data?.toolIoLogsEnabled === true &&
      features.data?.sessionCaptureEnabled === true);
  const featuresLoading = features.isLoading;
  const featuresFailed =
    !featuresLoading && Boolean(features.error || !features.data);
  const featuresReady =
    Boolean(features.data) && !featuresLoading && !featuresFailed;
  const canSkip = featuresReady && !loggingBundleEnabled && !bundleBusy;

  return (
    <StepContainer
      icon={
        <div className="bg-secondary flex h-12 w-12 items-center justify-center">
          <FileText className="text-foreground h-6 w-6" />
        </div>
      }
      title="Enable logging"
      description={ENABLE_LOGS_PAGE_DESCRIPTION}
      onContinue={onComplete}
      continueLabel="Continue"
      skipLabel="Skip for now"
      onSkip={canSkip ? onSkip : undefined}
      showBack
      onBack={onBack}
      canContinue={featuresReady && !bundleBusy}
      isLoading={featuresLoading || bundleBusy}
    >
      <div className="space-y-6">
        <LogDataRetentionBanner />
        <div className="border-border bg-card border p-4">
          <EnableLoggingAndSessionCaptureSetting
            onEnabledChange={setBundleEnabled}
            onBusyChange={setBundleBusy}
          />
        </div>
        <p className="text-muted-foreground text-sm">
          This turns on Enable Logs, Record Tool I/O, and Agent Session Capture
          — the same settings as{" "}
          <Link
            to={orgRoutes.logs.href()}
            className="underline underline-offset-2"
          >
            Logging &amp; Telemetry
          </Link>
          . Fail Open, Hook Browser Sign-In, and other options stay on that
          page.
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
