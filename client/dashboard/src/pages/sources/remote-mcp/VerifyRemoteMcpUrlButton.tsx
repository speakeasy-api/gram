import { Alert, Button } from "@speakeasy-api/moonshine";
import { Loader2, ShieldCheck } from "lucide-react";
import type { VerifyRemoteMcpUrlState } from "./useVerifyRemoteMcpUrl";

export function VerifyRemoteMcpUrlButton({
  state,
  url,
  disabled,
}: {
  state: VerifyRemoteMcpUrlState;
  url: string;
  disabled?: boolean;
}) {
  const buttonDisabled = disabled || state.isPending || !url.trim();

  return (
    <Button
      type="button"
      variant="secondary"
      disabled={buttonDisabled}
      onClick={() => {
        void state.trigger();
      }}
    >
      {state.isPending ? (
        <>
          <Button.LeftIcon>
            <Loader2 className="size-4 animate-spin" />
          </Button.LeftIcon>
          <Button.Text>Verifying</Button.Text>
        </>
      ) : (
        <>
          <Button.LeftIcon>
            <ShieldCheck className="size-4" />
          </Button.LeftIcon>
          <Button.Text>Verify MCP Connectivity</Button.Text>
        </>
      )}
    </Button>
  );
}

export function VerifyRemoteMcpUrlAlert({
  state,
}: {
  state: VerifyRemoteMcpUrlState;
}) {
  if (!state.result) return null;
  return (
    <Alert
      variant={state.result.verified ? "success" : "error"}
      dismissible={false}
    >
      {state.result.message}
    </Alert>
  );
}
