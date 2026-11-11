import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
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
  not_found: "Logs endpoint not found",
  unreachable: "Destination not reachable",
  error: "Destination returned an error",
};

function failureMessage(error: unknown): string {
  if (error instanceof GramError && error.statusCode === 429) {
    return "Too many test events for this export. Try again in a minute.";
  }
  return "Speakeasy could not send the test event. Try again.";
}

/**
 * Sends one test event to a saved MCP connection logs export's destination and
 * shows what it answered. Org admins only.
 */
export function SendTestEventSection({
  projectSlug,
  routeId,
  unsavedChanges,
}: {
  projectSlug: string;
  routeId: string;
  unsavedChanges: boolean;
}): JSX.Element {
  const sendTest = useTestDataExportRouteMutation();
  const result = sendTest.data;

  return (
    <RequireScope scope="org:admin" level="section">
      <div className="space-y-3 border-t py-5">
        <div className="flex items-start justify-between gap-4">
          <div className="space-y-1">
            <Label>Test event</Label>
            <Text muted className="text-sm">
              {unsavedChanges
                ? "Save your changes to send a test event to this destination."
                : "Send one event, marked as a test, to check the endpoint and API key."}
            </Text>
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
        {sendTest.isError ? (
          <Alert variant="error" title="Test event not sent">
            {failureMessage(sendTest.error)}
          </Alert>
        ) : null}
        {result && !sendTest.isPending && !sendTest.isError ? (
          <Alert
            variant={alertVariantForOutcome[result.outcome] ?? "warning"}
            title={
              alertTitleForOutcome[result.outcome] ??
              "Destination returned an error"
            }
          >
            {result.message}
          </Alert>
        ) : null}
      </div>
    </RequireScope>
  );
}
