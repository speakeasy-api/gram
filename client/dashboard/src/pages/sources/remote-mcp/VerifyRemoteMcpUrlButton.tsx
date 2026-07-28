import { Alert, Button } from "@speakeasy-api/moonshine";
import { Loader2, Plug } from "lucide-react";
import type { VerifyRemoteMcpUrlState } from "./useVerifyRemoteMcpUrl";

export function VerifyRemoteMcpUrlButton({
  state,
  url,
  disabled,
}: {
  state: VerifyRemoteMcpUrlState;
  url: string;
  disabled?: boolean;
}): JSX.Element {
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
      <Button.LeftIcon>
        {state.isPending ? (
          <Loader2 className="size-4 animate-spin" />
        ) : (
          <Plug className="size-4" />
        )}
      </Button.LeftIcon>
      <Button.Text>
        {state.isPending ? "Verifying" : "Verify MCP Connectivity"}
      </Button.Text>
    </Button>
  );
}

export function VerifyRemoteMcpUrlAlert({
  state,
}: {
  state: VerifyRemoteMcpUrlState;
}): JSX.Element | null {
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
