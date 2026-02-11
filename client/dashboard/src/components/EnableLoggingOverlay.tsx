import { useState } from "react";
import { Button, Icon } from "@speakeasy-api/moonshine";
import { FeatureName } from "@gram/client/models/components";
import { useFeaturesSetMutation } from "@gram/client/react-query";

interface EnableLoggingOverlayProps {
  onEnabled: () => void;
}

/**
 * Shared overlay component shown when logs are not enabled for the organization.
 * Displays a centered card with enable button and handles the mutation state.
 */
export function EnableLoggingOverlay({ onEnabled }: EnableLoggingOverlayProps) {
  const [mutationError, setMutationError] = useState<string | null>(null);
  const { mutate: setLogsFeature, status: mutationStatus } =
    useFeaturesSetMutation({
      onSuccess: () => {
        setMutationError(null);
        onEnabled();
      },
      onError: (err) => {
        const message =
          err instanceof Error ? err.message : "Failed to enable logging";
        setMutationError(message);
      },
    });

  const isMutating = mutationStatus === "pending";

  const handleEnable = () => {
    setMutationError(null);
    setLogsFeature({
      request: {
        setProductFeatureRequestBody: {
          featureName: FeatureName.Logs,
          enabled: true,
        },
      },
    });
  };

  return (
    <div className="absolute inset-0 z-10 flex items-center justify-center bg-background/70 backdrop-blur-[2px] rounded-lg">
      <div className="flex flex-col items-center gap-4 max-w-md text-center p-8">
        <div className="size-14 rounded-full bg-muted flex items-center justify-center">
          <Icon name="activity" className="size-7 text-muted-foreground" />
        </div>
        <div>
          <h3 className="text-lg font-semibold mb-1">Enable Logging</h3>
          <p className="text-sm text-muted-foreground">
            Turn on logging to start collecting telemetry data for your
            organization. This will record tool call traces, chat sessions, and
            system metrics to power the observability dashboard.
          </p>
        </div>
        <div className="rounded-lg border border-border bg-muted/30 p-4 text-left w-full">
          <div className="flex items-start gap-2">
            <Icon
              name="info"
              className="size-4 text-muted-foreground mt-0.5 shrink-0"
            />
            <p className="text-xs text-muted-foreground">
              When enabled, Gram will collect tool call payloads, response data,
              and chat conversation logs for analysis. This data is stored
              securely and used to generate the metrics and insights. You can
              disable logging at any time from the Logs page.
            </p>
          </div>
        </div>
        <Button onClick={handleEnable} disabled={isMutating}>
          <Button.LeftIcon>
            <Icon name="activity" className="size-4" />
          </Button.LeftIcon>
          <Button.Text>
            {isMutating ? "Enabling..." : "Enable Logging"}
          </Button.Text>
        </Button>
        {mutationError && (
          <span className="text-sm text-destructive">{mutationError}</span>
        )}
      </div>
    </div>
  );
}
