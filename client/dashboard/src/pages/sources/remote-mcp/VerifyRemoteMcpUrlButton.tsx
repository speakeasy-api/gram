import { cn } from "@/lib/utils";
import { AlertCircle, Check } from "lucide-react";
import type { VerifyRemoteMcpUrlState } from "./useVerifyRemoteMcpUrl";

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

  // "Try /mcp" is the right nudge when something answered and wasn't an MCP
  // endpoint. Transport failures are excluded first: a TLS or DNS error says
  // nothing about the path, and the message often carries the hostname — which
  // frequently contains "mcp" — so a keyword search alone points people at the
  // wrong thing.
  const isTransportFailure =
    /\b(tls|ssl|handshake|certificate|dns|timeout|timed out|refused|unreachable|network|resolve)\b/i.test(
      message,
    );
  const looksLikeWrongEndpoint =
    !isTransportFailure &&
    /(not an mcp|no mcp|404|not found|invalid response|unexpected (content|response)|initialize)/i.test(
      message,
    );

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
        {!verified && looksLikeWrongEndpoint && (
          <span className="text-muted-foreground">
            Check the URL points at the server&apos;s MCP endpoint — often it
            ends in /mcp.
          </span>
        )}
      </span>
    </div>
  );
}
