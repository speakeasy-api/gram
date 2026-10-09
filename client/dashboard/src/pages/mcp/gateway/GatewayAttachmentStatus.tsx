import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Link } from "react-router";
import type { GatewayCreationFlow } from "./useGatewayCreation";

export function GatewayAttachmentStatus({
  flow,
  serverHref,
}: {
  flow: GatewayCreationFlow;
  /**
   * Where the created server lives, linked once the gateway refuses it for
   * good. Omit when the page already offers its own way to open the server.
   */
  serverHref?: (mcpServerId: string) => string;
}): JSX.Element | null {
  if (!flow.attachmentError && !flow.isAttaching) return null;
  return (
    <Alert variant={flow.isAttaching ? "info" : "error"} dismissible={false}>
      {!flow.isAttaching && <p>{flow.attachmentError}</p>}
      {flow.attachmentRefused && flow.createdServerId && serverHref ? (
        <Button variant="secondary" asChild>
          <Link to={serverHref(flow.createdServerId)}>
            <Button.Text>Open MCP server</Button.Text>
          </Link>
        </Button>
      ) : null}
      {flow.attachmentRefused ? null : (
        <Button
          type="button"
          variant="secondary"
          disabled={flow.isAttaching}
          onClick={() => {
            void flow.retry().catch(() => {});
          }}
        >
          <Button.Text>
            {flow.isAttaching
              ? "Adding to gateway…"
              : "Retry adding to gateway"}
          </Button.Text>
        </Button>
      )}
    </Alert>
  );
}
