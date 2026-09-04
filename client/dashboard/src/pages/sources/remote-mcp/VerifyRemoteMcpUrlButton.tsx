import { Button } from "@/components/ui/Button";
import { cn } from "@/lib/utils";
import { AlertCircle, Check, Loader2, Plug } from "lucide-react";
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
  const { verified, message } = state.result;
  // Backend messages arrive in mixed case ("invalid url"); sentence-case them
  // so the field reads consistently without inventing new wording.
  const text = message.charAt(0).toUpperCase() + message.slice(1);

  // A field-level status line rather than a full-width alert box: this reports
  // on the input directly above it, and a banner at that weight reads as a page
  // problem. Matches the URL validation message's size and icon so the two
  // never argue about which one owns the field.
  return (
    <div
      role="status"
      className={cn(
        "mt-2 flex items-start gap-1.5 text-xs",
        verified ? "text-default-success" : "text-destructive",
      )}
    >
      {verified ? (
        <Check className="mt-px size-3.5 shrink-0" />
      ) : (
        <AlertCircle className="mt-px size-3.5 shrink-0" />
      )}
      <span className="flex flex-col gap-0.5">
        <span>{text}</span>
        {!verified && (
          <span className="text-muted-foreground">
            Check the URL points at the server&apos;s MCP endpoint — often it
            ends in /mcp.
          </span>
        )}
      </span>
    </div>
  );
}
