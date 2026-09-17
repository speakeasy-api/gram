import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import type { GatewayCreationFlow } from "./useGatewayCreation";

export function GatewayAttachmentStatus({
  flow,
}: {
  flow: GatewayCreationFlow;
}): JSX.Element | null {
  if (!flow.attachmentError && !flow.isAttaching) return null;
  return (
    <Alert variant={flow.isAttaching ? "info" : "error"} dismissible={false}>
      <p role="status">
        {flow.isAttaching ? "Adding to gateway…" : flow.attachmentError}
      </p>
      <Button
        type="button"
        variant="secondary"
        disabled={flow.isAttaching}
        onClick={() => {
          void flow.retry().catch(() => {});
        }}
      >
        <Button.Text>
          {flow.isAttaching ? "Adding to gateway…" : "Retry adding to gateway"}
        </Button.Text>
      </Button>
    </Alert>
  );
}
