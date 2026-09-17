import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import type { GatewayCreationFlow } from "./useGatewayCreation";

export function GatewayAttachmentStatus({
  flow,
}: {
  flow: GatewayCreationFlow;
}): JSX.Element | null {
  if (!flow.attachmentError) return null;
  return (
    <Alert variant="error" dismissible={false}>
      <p>{flow.attachmentError}</p>
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
