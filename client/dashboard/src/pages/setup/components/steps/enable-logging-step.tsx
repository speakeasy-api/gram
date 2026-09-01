import { useState } from "react";
import { Check, FileText, Info } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { FeatureName } from "@gram/client/models/components/setproductfeaturerequestbody.js";
import { useFeaturesSetMutation } from "@gram/client/react-query/featuresSet.js";
import {
  invalidateAllProductFeatures,
  useProductFeatures,
} from "@gram/client/react-query/productFeatures.js";
import { Button } from "@/components/ui/Button";
import { useOrganization } from "@/contexts/Auth";
import { useIsCurrentOrganization } from "@/hooks/useIsCurrentOrganization";
import { useRBAC } from "@/hooks/useRBAC";
import { LOG_DATA_RETENTION_MESSAGE } from "@/components/observe/LoggingPageHeader";
import { StepContainer } from "../step-container";

interface EnableLoggingStepProps {
  onComplete: () => void;
  onSkip: () => void;
  onBack: () => void;
}

const COLLECTED_ITEMS = [
  "Tool call traces and execution metadata",
  "Agent session metadata used for insights and usage",
  "System metrics that power the observability dashboard",
] as const;

export function EnableLoggingStep({
  onComplete,
  onSkip,
  onBack,
}: EnableLoggingStepProps): JSX.Element {
  const organization = useOrganization();
  const isCurrentOrganization = useIsCurrentOrganization(organization.id);

  return (
    <EnableLoggingStepInner
      key={organization.id}
      organizationId={organization.id}
      isCurrentOrganization={isCurrentOrganization}
      onComplete={onComplete}
      onSkip={onSkip}
      onBack={onBack}
    />
  );
}

function EnableLoggingStepInner({
  organizationId,
  isCurrentOrganization,
  onComplete,
  onSkip,
  onBack,
}: EnableLoggingStepProps & {
  organizationId: string;
  isCurrentOrganization: () => boolean;
}): JSX.Element {
  const queryClient = useQueryClient();
  const { hasScope } = useRBAC();
  const isAdmin = hasScope("org:admin");
  const [mutationError, setMutationError] = useState<string | null>(null);

  const features = useProductFeatures({ organizationId }, undefined, {
    throwOnError: false,
  });
  const logsEnabled = features.data?.logsEnabled === true;

  const { mutate: setLogsFeature, status: mutationStatus } =
    useFeaturesSetMutation({
      onSuccess: async () => {
        if (!isCurrentOrganization()) return;
        setMutationError(null);
        await invalidateAllProductFeatures(queryClient);
        if (!isCurrentOrganization()) return;
        onComplete();
      },
      onError: (err) => {
        if (!isCurrentOrganization()) return;
        const message =
          err instanceof Error ? err.message : "Failed to enable logging";
        setMutationError(message);
      },
    });

  const isMutating = mutationStatus === "pending";
  const featuresLoading = features.isLoading;
  const featuresFailed =
    !featuresLoading && Boolean(features.error || !features.data);

  const handleEnable = () => {
    setMutationError(null);
    setLogsFeature({
      request: {
        setProductFeatureRequestBody: {
          organizationId,
          featureName: FeatureName.Logs,
          enabled: true,
        },
      },
    });
  };

  if (logsEnabled) {
    return (
      <StepContainer
        icon={<StepIcon />}
        title="Enable logging"
        description="Logging is already on for this organization. Tool call traces and telemetry will be recorded for insights and logs."
        onContinue={onComplete}
        continueLabel="Continue"
        showBack
        onBack={onBack}
      >
        <AlreadyEnabledNotice />
      </StepContainer>
    );
  }

  return (
    <StepContainer
      icon={<StepIcon />}
      title="Enable logging"
      description="Logging is off by default. Turn it on so this rollout can collect tool call traces and telemetry. Speakeasy will not start recording until you opt in."
      onContinue={handleEnable}
      continueLabel="Enable logging"
      skipLabel="Skip for now"
      onSkip={onSkip}
      showBack
      onBack={onBack}
      canContinue={isAdmin && !featuresLoading && !featuresFailed}
      isLoading={isMutating || featuresLoading}
    >
      <LoggingConsentBody
        featuresFailed={featuresFailed}
        isFetching={features.isFetching}
        mutationError={mutationError}
        onRetry={() => void features.refetch()}
      />
    </StepContainer>
  );
}

function StepIcon(): JSX.Element {
  return (
    <div className="bg-secondary flex h-12 w-12 items-center justify-center">
      <FileText className="text-foreground h-6 w-6" />
    </div>
  );
}

function AlreadyEnabledNotice(): JSX.Element {
  return (
    <div className="bg-foreground/5 border-foreground/10 border p-4">
      <div className="flex items-start gap-3">
        <div className="bg-foreground mt-0.5 flex h-8 w-8 flex-shrink-0 items-center justify-center">
          <Check className="text-background h-4 w-4" />
        </div>
        <div>
          <p className="text-foreground text-sm font-medium">
            Product logging is enabled
          </p>
          <p className="text-muted-foreground mt-1 text-sm">
            You can change this later from Organization → Logs. Tool I/O
            payloads stay off until you enable them there separately.
          </p>
        </div>
      </div>
    </div>
  );
}

function LoggingConsentBody({
  featuresFailed,
  isFetching,
  mutationError,
  onRetry,
}: {
  featuresFailed: boolean;
  isFetching: boolean;
  mutationError: string | null;
  onRetry: () => void;
}): JSX.Element {
  return (
    <div className="space-y-6">
      <div className="border-border bg-card border">
        <div className="border-border border-b px-4 py-3">
          <p className="text-foreground text-sm font-medium">
            What enabling collects
          </p>
        </div>
        <ul className="divide-border divide-y">
          {COLLECTED_ITEMS.map((item) => (
            <li key={item} className="flex items-start gap-3 px-4 py-3">
              <Check className="text-foreground mt-0.5 h-4 w-4 shrink-0" />
              <span className="text-sm">{item}</span>
            </li>
          ))}
        </ul>
      </div>

      <div className="border-border bg-card border p-4">
        <div className="flex items-start gap-3">
          <Info className="text-muted-foreground mt-0.5 h-4 w-4 shrink-0" />
          <div className="space-y-2 text-sm">
            <p className="text-muted-foreground">
              This is an intentional opt-in. Enabling logging records a
              consequential amount of operational data for your organization.
            </p>
            <p className="text-muted-foreground">
              {LOG_DATA_RETENTION_MESSAGE} Full tool request and response
              payloads are a separate setting and stay off until you enable them
              from Organization → Logs.
            </p>
            <p className="text-muted-foreground">
              You can disable logging at any time from that same page.
            </p>
          </div>
        </div>
      </div>

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
            disabled={isFetching}
            onClick={onRetry}
          >
            {isFetching ? "Retrying…" : "Retry"}
          </Button>
        </div>
      ) : null}

      {mutationError ? (
        <p className="text-destructive text-sm">{mutationError}</p>
      ) : null}
    </div>
  );
}
