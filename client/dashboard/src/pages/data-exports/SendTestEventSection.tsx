import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Label } from "@/components/ui/Label";
import { Text } from "@/components/ui/Text";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useTestDataExportRouteMutation } from "@gram/client/react-query/testDataExportRoute.js";

const alertVariantForOutcome: Record<string, "success" | "error" | "warning"> =
  {
    accepted: "success",
    refused: "error",
    not_found: "error",
    unreachable: "error",
    error: "warning",
  };

const alertTitleForOutcome: Record<string, string> = {
  accepted: "Test event accepted",
  refused: "Test event refused",
  not_found: "Endpoint not found",
  unreachable: "Destination not reachable",
  error: "Destination returned an error",
};

const signalLabel: Record<string, string> = {
  logs: "Logs",
  traces: "Traces",
  metrics: "Metrics",
};

function outcomeTitle(outcome: string, signal?: string): string {
  if (outcome === "not_found" && signal) {
    return `${signalLabel[signal] ?? signal} endpoint not found`;
  }
  return alertTitleForOutcome[outcome] ?? "Destination returned an error";
}

// Only a 4xx is a refusal before sending; anything else may come after the event went out.
function failure(error: unknown): { title: string; message: string } {
  const status = error instanceof GramError ? error.statusCode : undefined;
  if (status === 429) {
    return {
      title: "Test event not sent",
      message:
        "Too many test events were sent just now. Try again in a minute.",
    };
  }
  if (status === 503) {
    return {
      title: "Test event not sent",
      message: "Speakeasy can't send test events right now. Try again shortly.",
    };
  }
  if (status !== undefined && status >= 400 && status < 500) {
    return {
      title: "Test event not sent",
      message: "Speakeasy could not send the test event. Try again.",
    };
  }
  return {
    title: "Test result unknown",
    message:
      "Speakeasy could not confirm the result. The test event may have been sent, so check your destination before trying again.",
  };
}

/**
 * Sends a saved export's test events to its destination and shows what it
 * answered for each signal. Org admins only.
 */
export function SendTestEventSection({
  projectSlug,
  routeId,
  sendsEverySignal,
  unsavedChanges,
}: {
  projectSlug: string;
  routeId: string;
  /** The export sends logs, traces and metrics, so the test does too. */
  sendsEverySignal: boolean;
  unsavedChanges: boolean;
}): JSX.Element {
  // The section shows its own error, so skip the app-wide error toast.
  const sendTest = useTestDataExportRouteMutation({ onError: () => {} });
  const result = sendTest.data;

  return (
    <RequireScope scope="org:admin" level="section">
      <div className="space-y-3 border-t py-5">
        <div className="flex items-center justify-between gap-4">
          <div className="space-y-1">
            <Label>Test event</Label>
            {unsavedChanges ? (
              <Text muted className="text-sm">
                Save your changes to send a test event to this destination.
              </Text>
            ) : (
              <Text muted className="text-sm">
                {sendsEverySignal
                  ? "Sends one test log, trace and metric to check the endpoint and API key."
                  : "Sends one test event to check the endpoint and API key."}{" "}
                Test events are marked{" "}
                <code className="font-mono text-xs">
                  speakeasy.test_event=true
                </code>{" "}
                and their message starts with &ldquo;Speakeasy test
                event&rdquo;, so you can filter them out of dashboards and
                monitors.
              </Text>
            )}
          </div>
          <Button
            type="button"
            variant="secondary"
            size="sm"
            disabled={unsavedChanges || sendTest.isPending}
            onClick={() =>
              sendTest.mutate({
                request: { id: routeId, gramProject: projectSlug },
              })
            }
          >
            {sendTest.isPending ? "Sending" : "Send test event"}
          </Button>
        </div>
        {sendTest.isError && !unsavedChanges ? (
          <Alert variant="error" title={failure(sendTest.error).title}>
            {failure(sendTest.error).message}
          </Alert>
        ) : null}
        {result &&
        !unsavedChanges &&
        !sendTest.isPending &&
        !sendTest.isError ? (
          <Alert
            variant={alertVariantForOutcome[result.outcome] ?? "warning"}
            title={
              result.signals.length > 1 && result.outcome !== "accepted"
                ? "Not every test event was accepted"
                : outcomeTitle(result.outcome, result.signals[0]?.signal)
            }
          >
            {result.message}
            {result.signals.length > 1 ? (
              <ul className="mt-2 space-y-1">
                {result.signals.map((signal) => (
                  <li key={signal.signal}>
                    <span className="font-medium">
                      {signalLabel[signal.signal] ?? signal.signal}:{" "}
                      {outcomeTitle(signal.outcome)}.
                    </span>{" "}
                    {signal.message}
                  </li>
                ))}
              </ul>
            ) : null}
          </Alert>
        ) : null}
        {result &&
        !unsavedChanges &&
        !sendTest.isPending &&
        !sendTest.isError ? (
          <div className="flex items-center gap-2">
            <Text muted className="text-sm">
              Find it in your destination:
            </Text>
            <code className="font-mono text-xs break-all">
              speakeasy.test_event.id={result.testEventId}
            </code>
            <CopyButton
              size="xs"
              text={`speakeasy.test_event.id=${result.testEventId}`}
              tooltip="Copy test event ID"
            />
          </div>
        ) : null}
      </div>
    </RequireScope>
  );
}
