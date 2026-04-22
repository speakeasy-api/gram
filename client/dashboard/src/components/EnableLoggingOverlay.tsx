import { RequireScope } from "@/components/require-scope";
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
        featureName: FeatureName.Logs,
        enabled: true,
      },
    });
  };

  return (
    <div className="bg-background/70 absolute inset-0 z-10 flex items-center justify-center rounded-lg backdrop-blur-[2px]">
      <div className="flex max-w-md flex-col items-center gap-4 p-8 text-center">
        <div className="bg-muted flex size-14 items-center justify-center rounded-full">
          <Icon name="activity" className="text-muted-foreground size-7" />
        </div>
        <div>
          <h3 className="mb-1 text-lg font-semibold">Enable Logging</h3>
          <p className="text-muted-foreground text-sm">
            Turn on logging to start collecting telemetry data for your
            organization. This will record tool call traces, agent sessions, and
            system metrics to power the observability dashboard.
          </p>
        </div>
        <div className="border-border bg-muted/30 w-full rounded-lg border p-4 text-left">
          <div className="flex items-start gap-2">
            <Icon
              name="info"
              className="text-muted-foreground mt-0.5 size-4 shrink-0"
            />
            <p className="text-muted-foreground text-xs">
              When enabled, Gram will collect tool call payloads, response data,
              and agent session logs for analysis. This data is stored securely
              and used to generate the metrics and insights. You can disable
              logging at any time from the Logs page.
            </p>
          </div>
        </div>
        <RequireScope scope="org:admin" level="component">
          <Button onClick={handleEnable} disabled={isMutating}>
            <Button.LeftIcon>
              <Icon name="activity" className="size-4" />
            </Button.LeftIcon>
            <Button.Text>
              {isMutating ? "Enabling..." : "Enable Logging"}
            </Button.Text>
          </Button>
        </RequireScope>
        {mutationError && (
          <span className="text-destructive text-sm">{mutationError}</span>
        )}
      </div>
    </div>
  );
}
