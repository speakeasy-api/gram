import { FeatureToggleRow } from "@/components/feature-toggle-row";
import { LogDataRetentionBanner } from "@/components/observe/LoggingPageHeader";
import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { handleAPIError } from "@/lib/errors";
import { FeatureName } from "@gram/client/models/components/setproductfeaturerequestbody.js";
import { useFeaturesSetMutation } from "@gram/client/react-query/featuresSet";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { Stack } from "@speakeasy-api/moonshine";
import { Eye, FileText, Monitor } from "lucide-react";
import { useState } from "react";
import { OtelForwardingSection } from "@/pages/org/OtelForwardingSection";
import { RedactionRulesSection } from "./RedactionRules";

// Data Configuration: everything that controls WHAT telemetry the platform
// captures, forwards, and stores — capture toggles, OTel forwarding, and
// redaction rules. Operational hook behavior (fail-open, browser sign-in)
// stays on the Hook Behavior settings page; those aren't data settings.
export function DataConfiguration(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["org:read", "org:admin"]} level="page">
          <DataConfigurationInner />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

function DataConfigurationInner() {
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

  const effectiveLogsEnabled =
    logsEnabled ?? featuresData?.logsEnabled ?? false;
  const effectiveToolIoLogsEnabled =
    toolIoLogsEnabled ?? featuresData?.toolIoLogsEnabled ?? false;
  const effectiveSessionCaptureEnabled =
    sessionCaptureEnabled ?? featuresData?.sessionCaptureEnabled ?? false;
  const effectiveSkillCaptureMetadataOnly =
    skillCaptureMetadataOnly ?? featuresData?.skillCaptureMetadataOnly ?? false;

  const { mutate: setFeature, status: mutationStatus } = useFeaturesSetMutation(
    {
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
        }
      },
      onError: (error) => {
        // On error the optimistic state above never runs, so the switch
        // reverts to the server value.
        handleAPIError(error, "Failed to update setting");
      },
    },
  );

  const isMutating = mutationStatus === "pending";

  const setNamedFeature = (featureName: FeatureName, enabled: boolean) => {
    setFeature({
      request: { setProductFeatureRequestBody: { featureName, enabled } },
    });
  };

  const handleSetLogs = (enabled: boolean) => {
    setNamedFeature(FeatureName.Logs, enabled);
    // Tool I/O recording depends on logs; turning logs off turns it off too.
    if (!enabled && effectiveToolIoLogsEnabled) {
      setNamedFeature(FeatureName.ToolIoLogs, false);
    }
  };

  return (
    <>
      <Heading variant="h4" className="mb-2">
        Data Capture
      </Heading>
      <Type muted small className="mb-6">
        Configure logging and telemetry settings for all your tool capture. When
        enabled, tool calls and traces are recorded for debugging and analytics.
        These power the insights and logs pages on the platform.
      </Type>
      <LogDataRetentionBanner />
      <div className="border-border bg-card rounded-lg border p-4">
        <Stack gap={4}>
          <FeatureToggleRow
            icon={FileText}
            title="Enable Logs"
            description="Record tool call traces and telemetry data"
            checked={effectiveLogsEnabled}
            onCheckedChange={handleSetLogs}
            disabled={isMutating}
            ready={!!featuresData}
            ariaLabel="Enable logs"
          />

          {featuresData?.skillsEnabled && (
            <>
              <div className="border-border border-t" />
              <FeatureToggleRow
                icon={FileText}
                title="Upload Skill Content"
                description="When enabled, Gram uploads SKILL.md content at activation so captured skills can be inspected. When disabled, Gram only receives skill names, source details, hashes, users, and hostnames at activation."
                checked={!effectiveSkillCaptureMetadataOnly}
                onCheckedChange={(enabled) =>
                  setNamedFeature(
                    FeatureName.SkillCaptureMetadataOnly,
                    !enabled,
                  )
                }
                disabled={isMutating}
                ready
                ariaLabel="Upload skill content"
              />
            </>
          )}

          <div className="border-border border-t" />

          <FeatureToggleRow
            icon={Eye}
            title="Record Tool I/O"
            description="Store tool inputs and outputs. May expose sensitive data in logs."
            checked={effectiveToolIoLogsEnabled}
            onCheckedChange={(enabled) =>
              setNamedFeature(FeatureName.ToolIoLogs, enabled)
            }
            disabled={isMutating || !effectiveLogsEnabled}
            ready={!!featuresData}
            ariaLabel="Record tool inputs and outputs"
          />

          <div className="border-border border-t" />

          <FeatureToggleRow
            icon={Monitor}
            title="Agent Session Capture"
            description="Capture user prompts and assistant responses from agents like Cursor, Claude Code, Codex, and more. Sessions appear in the Agent Sessions tab."
            checked={effectiveSessionCaptureEnabled}
            onCheckedChange={(enabled) =>
              setNamedFeature(FeatureName.SessionCapture, enabled)
            }
            disabled={isMutating || !effectiveLogsEnabled}
            ready={!!featuresData}
            ariaLabel="Enable agent session capture"
          />
        </Stack>
      </div>

      <div className="mt-8">
        <OtelForwardingSection />
      </div>

      <div className="mt-8">
        <RedactionRulesSection />
      </div>
    </>
  );
}
