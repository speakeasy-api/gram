import { Page } from "@/components/page-layout";
import { Heading } from "@/components/ui/heading";
import { Switch } from "@/components/ui/switch";
import { Type } from "@/components/ui/type";
import { FeatureName } from "@gram/client/models/components";
import { useFeaturesGet } from "@gram/client/react-query/featuresGet";
import { useFeaturesSetMutation } from "@gram/client/react-query/featuresSet";
import { Stack } from "@speakeasy-api/moonshine";
import { Eye, FileText } from "lucide-react";
import { useState } from "react";

export default function OrgLogs() {
  const { data: featuresData, isLoading: featuresLoading } = useFeaturesGet();
  const [logsEnabled, setLogsEnabled] = useState<boolean | null>(null);
  const [toolIoLogsEnabled, setToolIoLogsEnabled] = useState<boolean | null>(
    null,
  );

  const effectiveLogsEnabled =
    logsEnabled ?? featuresData?.logsEnabled ?? false;
  const effectiveToolIoLogsEnabled =
    toolIoLogsEnabled ?? featuresData?.toolIoLogsEnabled ?? false;

  const { mutate: setLogsFeature, status: logsMutationStatus } =
    useFeaturesSetMutation({
      onSuccess: (_, variables) => {
        if (
          variables.request.setProductFeatureRequestBody.featureName ===
          FeatureName.Logs
        ) {
          setLogsEnabled(
            variables.request.setProductFeatureRequestBody.enabled,
          );
        } else if (
          variables.request.setProductFeatureRequestBody.featureName ===
          FeatureName.ToolIoLogs
        ) {
          setToolIoLogsEnabled(
            variables.request.setProductFeatureRequestBody.enabled,
          );
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

  return (
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
                <Switch
                  checked={effectiveLogsEnabled}
                  onCheckedChange={handleSetLogs}
                  disabled={isMutatingLogs}
                  aria-label="Enable logs"
                />
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
                <Switch
                  checked={effectiveToolIoLogsEnabled}
                  onCheckedChange={handleSetToolIoLogs}
                  disabled={isMutatingLogs || !effectiveLogsEnabled}
                  aria-label="Record tool inputs and outputs"
                />
              )}
            </Stack>
          </Stack>
        </div>
      </Page.Body>
    </Page>
  );
}
