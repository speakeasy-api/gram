import { Page } from "@/components/page-layout";
import { Heading } from "@/components/ui/heading";
import { Switch } from "@/components/ui/switch";
import { Type } from "@/components/ui/type";
import { FeatureName } from "@gram/client/models/components";
import { useFeaturesGet } from "@gram/client/react-query/featuresGet";
import { useFeaturesSetMutation } from "@gram/client/react-query/featuresSet";
import { Stack } from "@speakeasy-api/moonshine";
import { Eye, FileText, Monitor } from "lucide-react";
import { useState } from "react";
import { RequireScope } from "@/components/require-scope";

export default function OrgLogs() {
  const { data: featuresData, isLoading: featuresLoading } = useFeaturesGet();
  const [logsEnabled, setLogsEnabled] = useState<boolean | null>(null);
  const [toolIoLogsEnabled, setToolIoLogsEnabled] = useState<boolean | null>(
    null,
  );
  const [sessionCaptureEnabled, setSessionCaptureEnabled] = useState<
    boolean | null
  >(null);

  const effectiveLogsEnabled =
    logsEnabled ?? featuresData?.logsEnabled ?? false;
  const effectiveToolIoLogsEnabled =
    toolIoLogsEnabled ?? featuresData?.toolIoLogsEnabled ?? false;
  const effectiveSessionCaptureEnabled =
    sessionCaptureEnabled ?? featuresData?.sessionCaptureEnabled ?? false;

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
        }
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

  return (
    <RequireScope scope={["org:read", "org:admin"]} level="page">
      <Page>
        <Page.Header>
          <Page.Header.Title>Logging & Telemetry</Page.Header.Title>
        </Page.Header>
        <Page.Body>
          <Heading variant="h4" className="mb-2">
            Logs
          </Heading>
          <Type muted small className="mb-6">
            Configure logging and telemetry settings for your MCP servers. When
            enabled, tool calls and traces are recorded for debugging and
            analytics.
          </Type>
          <div className="rounded-lg border border-border bg-card p-4">
            <Stack gap={4}>
              <Stack
                direction="horizontal"
                justify="space-between"
                align="center"
              >
                <Stack gap={1}>
                  <Stack direction="horizontal" align="center" gap={2}>
                    <FileText className="h-4 w-4 text-muted-foreground" />
                    <Type variant="body" className="font-medium">
                      Enable Logs
                    </Type>
                  </Stack>
                  <Type
                    variant="body"
                    className="text-muted-foreground text-sm ml-6"
                  >
                    Record tool call traces and telemetry data
                  </Type>
                </Stack>
                {!featuresLoading && (
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

              <div className="border-t border-border" />

              <Stack
                direction="horizontal"
                justify="space-between"
                align="center"
              >
                <Stack gap={1}>
                  <Stack direction="horizontal" align="center" gap={2}>
                    <Eye className="h-4 w-4 text-muted-foreground" />
                    <Type variant="body" className="font-medium">
                      Record Tool I/O
                    </Type>
                  </Stack>
                  <Type
                    variant="body"
                    className="text-muted-foreground text-sm ml-6"
                  >
                    Store tool inputs and outputs. May expose sensitive data in
                    logs.
                  </Type>
                </Stack>
                {!featuresLoading && (
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

              <div className="border-t border-border" />

              <Stack
                direction="horizontal"
                justify="space-between"
                align="center"
              >
                <Stack gap={1}>
                  <Stack direction="horizontal" align="center" gap={2}>
                    <Monitor className="h-4 w-4 text-muted-foreground" />
                    <Type variant="body" className="font-medium">
                      Claude Code Session Capture
                    </Type>
                  </Stack>
                  <Type
                    variant="body"
                    className="text-muted-foreground text-sm ml-6"
                  >
                    Capture user prompts and assistant responses from Claude
                    Code sessions. Sessions appear in the Agent Sessions tab.
                  </Type>
                </Stack>
                {!featuresLoading && (
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
            </Stack>
          </div>
        </Page.Body>
      </Page>
    </RequireScope>
  );
}
