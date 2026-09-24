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
import { useEffect, useRef, useState } from "react";

const ENABLE_LOGS_TITLE = "Enable Logs";
const ENABLE_LOGS_DESCRIPTION = "Record tool call traces and telemetry data";
export const ENABLE_LOGS_PAGE_DESCRIPTION =
  "Configure logging and telemetry settings for all your tool capture. When enabled, tool calls and traces are recorded for debugging and analytics. These power the insights and logs page on the platform.";

interface EnableLogsSettingProps {
  onEnabledChange?: (enabled: boolean) => void;
  onPendingChange?: (pending: boolean) => void;
}

/**
 * The organization "Enable Logs" switch from Logging & Telemetry.
 */
export function EnableLogsSetting({
  onEnabledChange,
  onPendingChange,
}: EnableLogsSettingProps = {}): JSX.Element | null {
  const organization = useOrganization();
  const isCurrentOrganization = useIsCurrentOrganization(organization.id);
  return (
    <EnableLogsSettingInner
      key={organization.id}
      organizationId={organization.id}
      isCurrentOrganization={isCurrentOrganization}
      onEnabledChange={onEnabledChange}
      onPendingChange={onPendingChange}
    />
  );
}

function EnableLogsSettingInner({
  organizationId,
  isCurrentOrganization,
  onEnabledChange,
  onPendingChange,
}: EnableLogsSettingProps & {
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

  const effectiveLogsEnabled =
    logsEnabled ?? features.data?.logsEnabled ?? false;
  const effectiveToolIoLogsEnabled =
    toolIoLogsEnabled ?? features.data?.toolIoLogsEnabled ?? false;

  const mutation = useFeaturesSetMutation({
    onSuccess: async (_, variables) => {
      if (!isCurrentOrganization()) return;
      const { featureName, enabled } =
        variables.request.setProductFeatureRequestBody;
      if (featureName === FeatureName.Logs) {
        setLogsEnabled(enabled);
        onEnabledChange?.(enabled);
      } else if (featureName === FeatureName.ToolIoLogs) {
        setToolIoLogsEnabled(enabled);
      }
      await invalidateAllProductFeatures(queryClient);
    },
    onError: (error) => {
      if (!isCurrentOrganization()) return;
      handleAPIError(error, "Failed to update setting");
    },
  });
  const [isSaving, setIsSaving] = useState(false);
  const onPendingChangeRef = useRef(onPendingChange);
  onPendingChangeRef.current = onPendingChange;

  useEffect(() => {
    onPendingChange?.(isSaving);
  }, [isSaving, onPendingChange]);

  useEffect(() => {
    return () => {
      onPendingChangeRef.current?.(false);
    };
  }, []);

  if (!features.data) return null;

  const handleSetLogs = (enabled: boolean) => {
    void (async () => {
      setIsSaving(true);
      try {
        await mutation.mutateAsync({
          request: {
            setProductFeatureRequestBody: {
              organizationId,
              featureName: FeatureName.Logs,
              enabled,
            },
          },
        });

        if (!enabled && effectiveToolIoLogsEnabled) {
          await mutation.mutateAsync({
            request: {
              setProductFeatureRequestBody: {
                organizationId,
                featureName: FeatureName.ToolIoLogs,
                enabled: false,
              },
            },
          });
        }
      } catch {
        // onError on the mutation reports the failure.
      } finally {
        if (isCurrentOrganization()) {
          setIsSaving(false);
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
            {ENABLE_LOGS_TITLE}
          </Text>
        </Stack>
        <Text variant="body" className="text-muted-foreground ml-6 text-sm">
          {ENABLE_LOGS_DESCRIPTION}
        </Text>
      </Stack>
      <RequireScope scope="org:admin" level="component">
        <Switch
          checked={effectiveLogsEnabled}
          onCheckedChange={handleSetLogs}
          disabled={isSaving || mutation.isPending}
          aria-label="Enable logs"
        />
      </RequireScope>
    </Stack>
  );
}
