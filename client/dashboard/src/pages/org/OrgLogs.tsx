import { Page } from "@/components/page-layout";
import { SettingsLayout } from "@/components/layouts/settings-layout";
import { RequireScope } from "@/components/require-scope";
import { Switch } from "@/components/ui/switch";
import { FeatureName } from "@gram/client/models/components/setproductfeaturerequestbody.js";
import { useFeaturesSetMutation } from "@gram/client/react-query/featuresSet";
import { useState } from "react";
import { AIIntegrationsSection } from "./AIIntegrationsSection";
import { OtelForwardingSection } from "./OtelForwardingSection";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { handleAPIError } from "@/lib/errors";

export default function OrgLogs(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["org:read", "org:admin"]} level="page">
          <OrgLogsInner />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

function OrgLogsInner() {
  const { data: featuresData, isLoading: featuresLoading } =
    useProductFeatures();
  const [logsEnabled, setLogsEnabled] = useState<boolean | null>(null);
  const [toolIoLogsEnabled, setToolIoLogsEnabled] = useState<boolean | null>(
    null,
  );
  const [sessionCaptureEnabled, setSessionCaptureEnabled] = useState<
    boolean | null
  >(null);
  const [observabilityModeEnabled, setObservabilityModeEnabled] = useState<
    boolean | null
  >(null);
  const [hooksBrowserLoginEnabled, setHooksBrowserLoginEnabled] = useState<
    boolean | null
  >(null);

  const effectiveLogsEnabled =
    logsEnabled ?? featuresData?.logsEnabled ?? false;
  const effectiveToolIoLogsEnabled =
    toolIoLogsEnabled ?? featuresData?.toolIoLogsEnabled ?? false;
  const effectiveSessionCaptureEnabled =
    sessionCaptureEnabled ?? featuresData?.sessionCaptureEnabled ?? false;
  const effectiveObservabilityModeEnabled =
    observabilityModeEnabled ?? featuresData?.observabilityModeEnabled ?? false;
  const effectiveHooksBrowserLoginEnabled =
    hooksBrowserLoginEnabled ?? featuresData?.hooksBrowserLoginEnabled ?? false;

  const { mutate: setLogsFeature, status: logsMutationStatus } =
    useFeaturesSetMutation({
      onSuccess: (_, variables) => {
        const { featureName, enabled } =
          variables.request.setProductFeatureRequestBody;
        if (featureName === FeatureName.Logs) {
          setLogsEnabled(enabled);
        } else if (featureName === FeatureName.ToolIoLogs) {
          setToolIoLogsEnabled(enabled);
        } else if (featureName === FeatureName.SessionCapture) {
          setSessionCaptureEnabled(enabled);
        } else if (featureName === FeatureName.ObservabilityMode) {
          setObservabilityModeEnabled(enabled);
        } else if (featureName === FeatureName.HooksBrowserLogin) {
          setHooksBrowserLoginEnabled(enabled);
        }
      },
      onError: (error) => {
        // Surfaces, among others, the phased-rollout block when an org toggles
        // observability mode before it's approved for the latest hooks version.
        // On error the optimistic state above never runs, so the switch reverts
        // to the server value.
        handleAPIError(error, "Failed to update setting");
      },
    });

  const isMutatingLogs = logsMutationStatus === "pending";

  const handleSetLogs = (enabled: boolean) => {
    setLogsFeature({
      request: {
        setProductFeatureRequestBody: {
          featureName: FeatureName.Logs,
          enabled,
        },
      },
    });

    if (!enabled && effectiveToolIoLogsEnabled) {
      setLogsFeature({
        request: {
          setProductFeatureRequestBody: {
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
          featureName: FeatureName.SessionCapture,
          enabled,
        },
      },
    });
  };

  const handleSetObservabilityMode = (enabled: boolean) => {
    setLogsFeature({
      request: {
        setProductFeatureRequestBody: {
          featureName: FeatureName.ObservabilityMode,
          enabled,
        },
      },
    });
  };

  const handleSetHooksBrowserLogin = (enabled: boolean) => {
    setLogsFeature({
      request: {
        setProductFeatureRequestBody: {
          featureName: FeatureName.HooksBrowserLogin,
          enabled,
        },
      },
    });
  };

  return (
    <SettingsLayout>
      <SettingsLayout.Header
        title="Logs"
        subtitle="Configure logging and telemetry settings for all your tool capture. When enabled, tool calls and traces are recorded for debugging and analytics. These power the insights and logs page on the platform."
      />
      <SettingsLayout.Body>
        <SettingsLayout.Group
          label="Enable Logs"
          description="Record tool call traces and telemetry data"
          actions={
            !featuresLoading && (
              <RequireScope scope="org:admin" level="component">
                <Switch
                  checked={effectiveLogsEnabled}
                  onCheckedChange={handleSetLogs}
                  disabled={isMutatingLogs}
                  aria-label="Enable logs"
                />
              </RequireScope>
            )
          }
        />

        <SettingsLayout.Group
          label="Record Tool I/O"
          description="Store tool inputs and outputs. May expose sensitive data in logs."
          actions={
            !featuresLoading && (
              <RequireScope scope="org:admin" level="component">
                <Switch
                  checked={effectiveToolIoLogsEnabled}
                  onCheckedChange={handleSetToolIoLogs}
                  disabled={isMutatingLogs || !effectiveLogsEnabled}
                  aria-label="Record tool inputs and outputs"
                />
              </RequireScope>
            )
          }
        />

        <SettingsLayout.Group
          label="Agent Session Capture"
          description="Capture user prompts and assistant responses from agents like Cursor, Claude Code, Codex, and more. Sessions appear in the Agent Sessions tab."
          actions={
            !featuresLoading && (
              <RequireScope scope="org:admin" level="component">
                <Switch
                  checked={effectiveSessionCaptureEnabled}
                  onCheckedChange={handleSetSessionCapture}
                  disabled={isMutatingLogs || !effectiveLogsEnabled}
                  aria-label="Enable Claude Code session capture"
                />
              </RequireScope>
            )
          }
        />

        <SettingsLayout.Group
          label="Observability Mode"
          description="Make generated hook plugins fully non-blocking. Hooks only observe and report, and can never deny or delay a tool call."
          actions={
            !featuresLoading && (
              <RequireScope scope="org:admin" level="component">
                <Switch
                  checked={effectiveObservabilityModeEnabled}
                  onCheckedChange={handleSetObservabilityMode}
                  disabled={isMutatingLogs}
                  aria-label="Enable observability mode"
                />
              </RequireScope>
            )
          }
        />

        <SettingsLayout.Group
          label="Hook Browser Sign-In"
          description="Let hook plugins sign users in through the browser to record events under their own identity. When off, plugins use the organization key or explicitly configured credentials."
          actions={
            !featuresLoading && (
              <RequireScope scope="org:admin" level="component">
                <Switch
                  checked={effectiveHooksBrowserLoginEnabled}
                  onCheckedChange={handleSetHooksBrowserLogin}
                  disabled={isMutatingLogs}
                  aria-label="Enable hook browser sign-in"
                />
              </RequireScope>
            )
          }
        />

        <AIIntegrationsSection />

        <OtelForwardingSection />
      </SettingsLayout.Body>
    </SettingsLayout>
  );
}
