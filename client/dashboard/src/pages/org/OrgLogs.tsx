import { SettingsPage } from "@/components/page-templates";
import { useOrganization } from "@/contexts/Auth";
import { LogDataRetentionBanner } from "@/components/observe/LoggingPageHeader";
import { RequireScope } from "@/components/require-scope";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { FeatureName } from "@gram/client/models/components/setproductfeaturerequestbody.js";
import { useFeaturesSetMutation } from "@gram/client/react-query/featuresSet";
import { Stack } from "@/components/ui/Stack";
import { Eye, FileText, LogIn, Monitor, Unplug } from "lucide-react";
import { useState } from "react";
import { OtelForwardingSection } from "./OtelForwardingSection";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { handleAPIError } from "@/lib/errors";
import { SkillContentUploadSetting } from "./SkillContentUploadSetting";
import { useIsCurrentOrganization } from "@/hooks/useIsCurrentOrganization";

export default function OrgLogs(): JSX.Element {
  const organization = useOrganization();
  const isCurrentOrganization = useIsCurrentOrganization(organization.id);
  return (
    <OrgLogsInner
      key={organization.id}
      organizationId={organization.id}
      isCurrentOrganization={isCurrentOrganization}
    />
  );
}

function OrgLogsInner({
  organizationId,
  isCurrentOrganization,
}: {
  organizationId: string;
  isCurrentOrganization: () => boolean;
}) {
  const { data: featuresData } = useProductFeatures({ organizationId });
  const [logsEnabled, setLogsEnabled] = useState<boolean | null>(null);
  const [toolIoLogsEnabled, setToolIoLogsEnabled] = useState<boolean | null>(
    null,
  );
  const [sessionCaptureEnabled, setSessionCaptureEnabled] = useState<
    boolean | null
  >(null);
  const [hooksBrowserLoginEnabled, setHooksBrowserLoginEnabled] = useState<
    boolean | null
  >(null);
  const [hooksFailOpenEnabled, setHooksFailOpenEnabled] = useState<
    boolean | null
  >(null);

  const effectiveLogsEnabled =
    logsEnabled ?? featuresData?.logsEnabled ?? false;
  const effectiveToolIoLogsEnabled =
    toolIoLogsEnabled ?? featuresData?.toolIoLogsEnabled ?? false;
  const effectiveSessionCaptureEnabled =
    sessionCaptureEnabled ?? featuresData?.sessionCaptureEnabled ?? false;
  const effectiveHooksBrowserLoginEnabled =
    hooksBrowserLoginEnabled ?? featuresData?.hooksBrowserLoginEnabled ?? false;
  const effectiveHooksFailOpenEnabled =
    hooksFailOpenEnabled ?? featuresData?.hooksFailOpenEnabled ?? false;

  const { mutate: setLogsFeature, status: logsMutationStatus } =
    useFeaturesSetMutation({
      onSuccess: (_, variables) => {
        if (!isCurrentOrganization()) return;
        const { featureName, enabled } =
          variables.request.setProductFeatureRequestBody;
        if (featureName === FeatureName.Logs) {
          setLogsEnabled(enabled);
        } else if (featureName === FeatureName.ToolIoLogs) {
          setToolIoLogsEnabled(enabled);
        } else if (featureName === FeatureName.SessionCapture) {
          setSessionCaptureEnabled(enabled);
        } else if (featureName === FeatureName.HooksBrowserLogin) {
          setHooksBrowserLoginEnabled(enabled);
        } else if (featureName === FeatureName.HooksFailOpen) {
          setHooksFailOpenEnabled(enabled);
        }
      },
      onError: (error) => {
        if (!isCurrentOrganization()) return;
        // On error the optimistic state above never runs, so the switch
        // reverts to the server value.
        handleAPIError(error, "Failed to update setting");
      },
    });

  const isMutatingLogs = logsMutationStatus === "pending";

  const handleSetLogs = (enabled: boolean) => {
    setLogsFeature({
      request: {
        setProductFeatureRequestBody: {
          organizationId,
          featureName: FeatureName.Logs,
          enabled,
        },
      },
    });

    if (!enabled && effectiveToolIoLogsEnabled) {
      setLogsFeature({
        request: {
          setProductFeatureRequestBody: {
            organizationId,
            featureName: FeatureName.ToolIoLogs,
            enabled: false,
          },
        },
      });
    }
  };

  const handleSetToolIoLogs = (enabled: boolean) => {
    setLogsFeature({
      request: {
        setProductFeatureRequestBody: {
          organizationId,
          featureName: FeatureName.ToolIoLogs,
          enabled,
        },
      },
    });
  };

  const handleSetSessionCapture = (enabled: boolean) => {
    setLogsFeature({
      request: {
        setProductFeatureRequestBody: {
          organizationId,
          featureName: FeatureName.SessionCapture,
          enabled,
        },
      },
    });
  };

  const handleSetHooksBrowserLogin = (enabled: boolean) => {
    setLogsFeature({
      request: {
        setProductFeatureRequestBody: {
          organizationId,
          featureName: FeatureName.HooksBrowserLogin,
          enabled,
        },
      },
    });
  };

  const handleSetHooksFailOpen = (enabled: boolean) => {
    setLogsFeature({
      request: {
        setProductFeatureRequestBody: {
          organizationId,
          featureName: FeatureName.HooksFailOpen,
          enabled,
        },
      },
    });
  };

  return (
    <SettingsPage
      scope={["org:read", "org:admin"]}
      title="Logs"
      description="Configure logging and telemetry settings for all your tool capture. When enabled, tool calls and traces are recorded for debugging and analytics. These power the insights and logs page on the platform."
    >
      <LogDataRetentionBanner />
      <div className="border-border bg-card border p-4">
        <Stack gap={4}>
          <Stack direction="horizontal" justify="space-between" align="center">
            <Stack gap={1}>
              <Stack direction="horizontal" align="center" gap={2}>
                <FileText className="text-muted-foreground h-4 w-4" />
                <Text variant="body" className="font-medium">
                  Enable Logs
                </Text>
              </Stack>
              <Text
                variant="body"
                className="text-muted-foreground ml-6 text-sm"
              >
                Record tool call traces and telemetry data
              </Text>
            </Stack>
            {featuresData && (
              <RequireScope scope="org:admin" level="component">
                <Switch
                  checked={effectiveLogsEnabled}
                  onCheckedChange={handleSetLogs}
                  disabled={isMutatingLogs}
                  aria-label="Enable logs"
                />
              </RequireScope>
            )}
          </Stack>

          <div className="border-border border-t" />

          <SkillContentUploadSetting />
          {featuresData && <div className="border-border border-t" />}

          <Stack direction="horizontal" justify="space-between" align="center">
            <Stack gap={1}>
              <Stack direction="horizontal" align="center" gap={2}>
                <Eye className="text-muted-foreground h-4 w-4" />
                <Text variant="body" className="font-medium">
                  Record Tool I/O
                </Text>
              </Stack>
              <Text
                variant="body"
                className="text-muted-foreground ml-6 text-sm"
              >
                Store tool inputs and outputs. May expose sensitive data in
                logs.
              </Text>
            </Stack>
            {featuresData && (
              <RequireScope scope="org:admin" level="component">
                <Switch
                  checked={effectiveToolIoLogsEnabled}
                  onCheckedChange={handleSetToolIoLogs}
                  disabled={isMutatingLogs || !effectiveLogsEnabled}
                  aria-label="Record tool inputs and outputs"
                />
              </RequireScope>
            )}
          </Stack>

          <div className="border-border border-t" />

          <Stack direction="horizontal" justify="space-between" align="center">
            <Stack gap={1}>
              <Stack direction="horizontal" align="center" gap={2}>
                <Monitor className="text-muted-foreground h-4 w-4" />
                <Text variant="body" className="font-medium">
                  Agent Session Capture
                </Text>
              </Stack>
              <Text
                variant="body"
                className="text-muted-foreground ml-6 text-sm"
              >
                Capture user prompts and assistant responses from supported
                coding agents. Sessions appear in the Agent Sessions tab.
              </Text>
            </Stack>
            {featuresData && (
              <RequireScope scope="org:admin" level="component">
                <Switch
                  checked={effectiveSessionCaptureEnabled}
                  onCheckedChange={handleSetSessionCapture}
                  disabled={isMutatingLogs || !effectiveLogsEnabled}
                  aria-label="Enable Claude Code session capture"
                />
              </RequireScope>
            )}
          </Stack>

          <div className="border-border border-t" />

          <Stack direction="horizontal" justify="space-between" align="center">
            <Stack gap={1}>
              <Stack direction="horizontal" align="center" gap={2}>
                <Unplug className="text-muted-foreground h-4 w-4" />
                <Text variant="body" className="font-medium">
                  Fail Open During Outages
                </Text>
              </Stack>
              <Text
                variant="body"
                className="text-muted-foreground mr-8 ml-6 max-w-4xl text-sm"
              >
                Let tool calls proceed while Speakeasy is unreachable, instead
                of blocking them (the default). Blocking policies go unenforced
                during the outage; events are still recorded and scanned after
                recovery. Invalid credentials always block.
              </Text>
            </Stack>
            {featuresData && (
              <RequireScope scope="org:admin" level="component">
                <Switch
                  checked={effectiveHooksFailOpenEnabled}
                  onCheckedChange={handleSetHooksFailOpen}
                  disabled={isMutatingLogs}
                  aria-label="Fail open during outages"
                />
              </RequireScope>
            )}
          </Stack>

          <div className="border-border border-t" />

          <Stack direction="horizontal" justify="space-between" align="center">
            <Stack gap={1}>
              <Stack direction="horizontal" align="center" gap={2}>
                <LogIn className="text-muted-foreground h-4 w-4" />
                <Text variant="body" className="font-medium">
                  Hook Browser Sign-In
                </Text>
              </Stack>
              <Text
                variant="body"
                className="text-muted-foreground ml-6 text-sm"
              >
                Let hook plugins sign users in through the browser to record
                events under their own identity. When off, plugins use the
                organization key or explicitly configured credentials.
              </Text>
            </Stack>
            {featuresData && (
              <RequireScope scope="org:admin" level="component">
                <Switch
                  checked={effectiveHooksBrowserLoginEnabled}
                  onCheckedChange={handleSetHooksBrowserLogin}
                  disabled={isMutatingLogs}
                  aria-label="Enable hook browser sign-in"
                />
              </RequireScope>
            )}
          </Stack>
        </Stack>
      </div>

      <OtelForwardingSection />
    </SettingsPage>
  );
}
