import { RequireScope } from "@/components/require-scope";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { Stack } from "@/components/ui/Stack";
import { useOrganization } from "@/contexts/Auth";
import { useIsCurrentOrganization } from "@/hooks/useIsCurrentOrganization";
import { handleAPIError } from "@/lib/errors";
import { FeatureName } from "@gram/client/models/components/setproductfeaturerequestbody.js";
import { useFeaturesSetMutation } from "@gram/client/react-query/featuresSet.js";
import {
  invalidateAllProductFeatures,
  useProductFeatures,
} from "@gram/client/react-query/productFeatures.js";
import { useQueryClient } from "@tanstack/react-query";
import { FileText } from "lucide-react";
import { useEffect, useState } from "react";

const TITLE = "Enable logging and session capture";
const DESCRIPTION =
  "Turns on Enable Logs, Record Tool I/O, and Agent Session Capture. Tool calls, request and response bodies, and agent session prompts are recorded.";

/** Features flipped together by the setup onboarding toggle. */
const LOGGING_BUNDLE_FEATURES = [
  FeatureName.Logs,
  FeatureName.ToolIoLogs,
  FeatureName.SessionCapture,
] as const;

const ENABLE_ORDER = LOGGING_BUNDLE_FEATURES;
const DISABLE_ORDER = [
  FeatureName.ToolIoLogs,
  FeatureName.SessionCapture,
  FeatureName.Logs,
] as const;

interface EnableLoggingAndSessionCaptureSettingProps {
  onEnabledChange?: (enabled: boolean) => void;
  onBusyChange?: (busy: boolean) => void;
}

/**
 * Setup-only switch that enables or disables the logging bundle used on
 * Organization → Logging & Telemetry: Enable Logs, Record Tool I/O, and
 * Agent Session Capture.
 */
export function EnableLoggingAndSessionCaptureSetting({
  onEnabledChange,
  onBusyChange,
}: EnableLoggingAndSessionCaptureSettingProps = {}): JSX.Element | null {
  const organization = useOrganization();
  const isCurrentOrganization = useIsCurrentOrganization(organization.id);
  return (
    <EnableLoggingAndSessionCaptureSettingInner
      key={organization.id}
      organizationId={organization.id}
      isCurrentOrganization={isCurrentOrganization}
      onEnabledChange={onEnabledChange}
      onBusyChange={onBusyChange}
    />
  );
}

function EnableLoggingAndSessionCaptureSettingInner({
  organizationId,
  isCurrentOrganization,
  onEnabledChange,
  onBusyChange,
}: EnableLoggingAndSessionCaptureSettingProps & {
  organizationId: string;
  isCurrentOrganization: () => boolean;
}): JSX.Element | null {
  const queryClient = useQueryClient();
  const features = useProductFeatures({ organizationId }, undefined, {
    throwOnError: false,
  });
  const [logsEnabled, setLogsEnabled] = useState<boolean | null>(null);
  const [toolIoLogsEnabled, setToolIoLogsEnabled] = useState<boolean | null>(
    null,
  );
  const [sessionCaptureEnabled, setSessionCaptureEnabled] = useState<
    boolean | null
  >(null);

  const effectiveLogsEnabled =
    logsEnabled ?? features.data?.logsEnabled ?? false;
  const effectiveToolIoLogsEnabled =
    toolIoLogsEnabled ?? features.data?.toolIoLogsEnabled ?? false;
  const effectiveSessionCaptureEnabled =
    sessionCaptureEnabled ?? features.data?.sessionCaptureEnabled ?? false;
  const bundleEnabled =
    effectiveLogsEnabled &&
    effectiveToolIoLogsEnabled &&
    effectiveSessionCaptureEnabled;

  const mutation = useFeaturesSetMutation();
  const [isSaving, setIsSaving] = useState(false);

  useEffect(() => {
    onBusyChange?.(isSaving);
    return () => {
      onBusyChange?.(false);
    };
  }, [isSaving, onBusyChange]);

  if (!features.data) return null;

  const isFeatureEnabled = (
    featureName: (typeof LOGGING_BUNDLE_FEATURES)[number],
  ): boolean => {
    switch (featureName) {
      case FeatureName.Logs:
        return effectiveLogsEnabled;
      case FeatureName.ToolIoLogs:
        return effectiveToolIoLogsEnabled;
      case FeatureName.SessionCapture:
        return effectiveSessionCaptureEnabled;
    }
  };

  const handleSetBundle = (enabled: boolean) => {
    void (async () => {
      setIsSaving(true);
      const order = enabled ? ENABLE_ORDER : DISABLE_ORDER;
      try {
        for (const featureName of order) {
          if (isFeatureEnabled(featureName) === enabled) continue;
          await mutation.mutateAsync({
            request: {
              setProductFeatureRequestBody: {
                organizationId,
                featureName,
                enabled,
              },
            },
          });
        }
        if (!isCurrentOrganization()) return;
        setLogsEnabled(enabled);
        setToolIoLogsEnabled(enabled);
        setSessionCaptureEnabled(enabled);
        onEnabledChange?.(enabled);
      } catch (error) {
        if (!isCurrentOrganization()) return;
        handleAPIError(error, "Failed to update setting");
      } finally {
        setIsSaving(false);
        if (isCurrentOrganization()) {
          await invalidateAllProductFeatures(queryClient);
        }
      }
    })();
  };

  return (
    <Stack direction="horizontal" justify="space-between" align="center">
      <Stack gap={1}>
        <Stack direction="horizontal" align="center" gap={2}>
          <FileText className="text-muted-foreground h-4 w-4" />
          <Text variant="body" className="font-medium">
            {TITLE}
          </Text>
        </Stack>
        <Text variant="body" className="text-muted-foreground ml-6 text-sm">
          {DESCRIPTION}
        </Text>
      </Stack>
      <RequireScope scope="org:admin" level="component">
        <Switch
          checked={bundleEnabled}
          onCheckedChange={handleSetBundle}
          disabled={isSaving || mutation.isPending}
          aria-label="Enable logging and session capture"
        />
      </RequireScope>
    </Stack>
  );
}
