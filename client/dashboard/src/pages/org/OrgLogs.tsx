import { Page } from "@/components/page-layout";
import { LogDataRetentionTooltip } from "@/components/observe/LoggingPageHeader";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/heading";
import { Switch } from "@/components/ui/switch";
import { Type } from "@/components/ui/type";
import { FeatureName } from "@gram/client/models/components/setproductfeaturerequestbody.js";
import { useFeaturesSetMutation } from "@gram/client/react-query/featuresSet";
import { Stack } from "@speakeasy-api/moonshine";
import { Eye, FileText, LogIn, Monitor, Unplug } from "lucide-react";
import { useState } from "react";
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
  const { data: featuresData } = useProductFeatures();
  const [logsEnabled, setLogsEnabled] = useState<boolean | null>(null);
  const [toolIoLogsEnabled, setToolIoLogsEnabled] = useState<boolean | null>(
    null,
  );
  const [sessionCaptureEnabled, setSessionCaptureEnabled] = useState<
    boolean | null
  >(null);
  const [skillCaptureMetadataOnly, setSkillCaptureMetadataOnly] = useState<
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
  const effectiveSkillCaptureMetadataOnly =
    skillCaptureMetadataOnly ?? featuresData?.skillCaptureMetadataOnly ?? false;
  const effectiveHooksBrowserLoginEnabled =
    hooksBrowserLoginEnabled ?? featuresData?.hooksBrowserLoginEnabled ?? false;
  const effectiveHooksFailOpenEnabled =
    hooksFailOpenEnabled ?? featuresData?.hooksFailOpenEnabled ?? false;

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
        } else if (featureName === FeatureName.SkillCaptureMetadataOnly) {
          setSkillCaptureMetadataOnly(enabled);
        } else if (featureName === FeatureName.HooksBrowserLogin) {
          setHooksBrowserLoginEnabled(enabled);
        } else if (featureName === FeatureName.HooksFailOpen) {
          setHooksFailOpenEnabled(enabled);
        }
      },
      onError: (error) => {
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

  const handleSetSkillCaptureMetadataOnly = (enabled: boolean) => {
    setLogsFeature({
      request: {
        setProductFeatureRequestBody: {
          featureName: FeatureName.SkillCaptureMetadataOnly,
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

  const handleSetHooksFailOpen = (enabled: boolean) => {
    setLogsFeature({
      request: {
        setProductFeatureRequestBody: {
          featureName: FeatureName.HooksFailOpen,
          enabled,
        },
      },
    });
  };

  return (
    <>
      <div className="mb-2 flex items-center gap-1.5">
        <Heading variant="h4">Logs</Heading>
        <LogDataRetentionTooltip />
      </div>
      <Type muted small className="mb-6">
        Configure logging and telemetry settings for all your tool capture. When
        enabled, tool calls and traces are recorded for debugging and analytics.
        These power the insights and logs page on the platform.
      </Type>
      <div className="border-border bg-card rounded-lg border p-4">
        <Stack gap={4}>
          <Stack direction="horizontal" justify="space-between" align="center">
            <Stack gap={1}>
              <Stack direction="horizontal" align="center" gap={2}>
                <FileText className="text-muted-foreground h-4 w-4" />
                <Type variant="body" className="font-medium">
                  Enable Logs
                </Type>
              </Stack>
              <Type
                variant="body"
                className="text-muted-foreground ml-6 text-sm"
              >
                Record tool call traces and telemetry data
              </Type>
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

          {featuresData?.skillsEnabled && (
            <>
              <Stack
                direction="horizontal"
                justify="space-between"
                align="center"
              >
                <Stack gap={1}>
                  <Stack direction="horizontal" align="center" gap={2}>
                    <FileText className="text-muted-foreground h-4 w-4" />
                    <Type variant="body" className="font-medium">
                      Upload Skill Content
                    </Type>
                  </Stack>
                  <Type
                    variant="body"
                    className="text-muted-foreground mr-8 ml-6 max-w-4xl text-sm"
                  >
                    When enabled, Gram uploads SKILL.md content at activation so
                    captured skills can be inspected. When disabled, Gram only
                    receives skill names, source details, hashes, users, and
                    hostnames at activation.
                  </Type>
                </Stack>
                <RequireScope scope="org:admin" level="component">
                  <Switch
                    checked={!effectiveSkillCaptureMetadataOnly}
                    onCheckedChange={(enabled) =>
                      handleSetSkillCaptureMetadataOnly(!enabled)
                    }
                    disabled={isMutatingLogs}
                    aria-label="Upload skill content"
                  />
                </RequireScope>
              </Stack>
              <div className="border-border border-t" />
            </>
          )}

          <Stack direction="horizontal" justify="space-between" align="center">
            <Stack gap={1}>
              <Stack direction="horizontal" align="center" gap={2}>
                <Eye className="text-muted-foreground h-4 w-4" />
                <Type variant="body" className="font-medium">
                  Record Tool I/O
                </Type>
              </Stack>
              <Type
                variant="body"
                className="text-muted-foreground ml-6 text-sm"
              >
                Store tool inputs and outputs. May expose sensitive data in
                logs.
              </Type>
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
                <Type variant="body" className="font-medium">
                  Agent Session Capture
                </Type>
              </Stack>
              <Type
                variant="body"
                className="text-muted-foreground ml-6 text-sm"
              >
                Capture user prompts and assistant responses from agents like
                Cursor, Claude Code, Codex, and more. Sessions appear in the
                Agent Sessions tab.
              </Type>
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
                <Type variant="body" className="font-medium">
                  Fail Open During Outages
                </Type>
              </Stack>
              <Type
                variant="body"
                className="text-muted-foreground mr-8 ml-6 max-w-4xl text-sm"
              >
                Let tool calls proceed while Speakeasy is unreachable, instead
                of blocking them (the default). Blocking policies go unenforced
                during the outage; events are still recorded and scanned after
                recovery. Invalid credentials always block.
              </Type>
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
                <Type variant="body" className="font-medium">
                  Hook Browser Sign-In
                </Type>
              </Stack>
              <Type
                variant="body"
                className="text-muted-foreground ml-6 text-sm"
              >
                Let hook plugins sign users in through the browser to record
                events under their own identity. When off, plugins use the
                organization key or explicitly configured credentials.
              </Type>
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

      <div className="mt-8">
        <OtelForwardingSection />
      </div>
    </>
  );
}
