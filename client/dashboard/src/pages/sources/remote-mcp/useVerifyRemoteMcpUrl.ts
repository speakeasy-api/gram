import { useProbeRemoteMcpURLMutation } from "@gram/client/react-query/probeRemoteMcpURL.js";
import { useEffect, useRef, useState } from "react";

type VerifyResult = {
  verified: boolean;
  message: string;
  outcome?: "mcp_available" | "authentication_required";
};

function unreachableMessage(reason: string | undefined): string {
  switch (reason) {
    case "timeout":
      return "Request timed out";
    case "rate_limited":
      return "Remote server rate limited the request";
    case "server_error":
      return "Remote server returned an error";
    case "dns_error":
      return "DNS lookup failed";
    case "tls_error":
      return "TLS connection failed";
    case "guardian_rejected":
      return "Network policy rejected the host";
    case undefined:
      return "Could not connect to the remote server";
    default:
      return "Could not connect to the remote server";
  }
}

export type VerifyRemoteMcpUrlState = {
  trigger: () => Promise<void>;
  result: VerifyResult | null;
  isPending: boolean;
};

// useVerifyRemoteMcpUrl owns the verify mutation plus its last result, and
// auto-clears the result whenever the URL changes so a stale verdict is never
// shown for a different URL. The state is split from the rendered Button +
// Alert so callers can place the button inside a row of actions and the alert
// next to the input it describes.
export function useVerifyRemoteMcpUrl(url: string): VerifyRemoteMcpUrlState {
  const probe = useProbeRemoteMcpURLMutation();
  const [result, setResult] = useState<VerifyResult | null>(null);
  const resultUrlRef = useRef<string | null>(null);
  // The URL on screen right now. A verify started for one URL must not answer
  // for another: edit the field mid-flight and the reply would otherwise land
  // as a verdict on what is now a different, unverified URL.
  const latestUrlRef = useRef(url);
  // Written during render, not in an effect: a reply can land between the
  // keystroke and the effect, and would find the ref still holding the URL the
  // user has already moved off.
  latestUrlRef.current = url;

  useEffect(() => {
    if (resultUrlRef.current !== null && resultUrlRef.current !== url) {
      setResult(null);
      resultUrlRef.current = null;
    }
  }, [url]);

  const trigger = async () => {
    const trimmed = url.trim();
    if (!trimmed) return;
    try {
      const response = await probe.mutateAsync({
        request: {
          probeURLForm: {
            url: trimmed,
          },
        },
      });
      if (latestUrlRef.current.trim() !== trimmed) return;
      switch (response.outcome) {
        case "mcp_available":
          setResult({
            verified: true,
            message: "MCP server is available",
            outcome: response.outcome,
          });
          break;
        case "authentication_required":
          setResult({
            verified: true,
            message: "MCP server is available and requires authentication",
            outcome: response.outcome,
          });
          break;
        case "invalid_mcp_response":
          let status = "";
          if (response.httpStatus !== undefined) {
            status = ` (HTTP ${response.httpStatus})`;
          }
          setResult({
            verified: false,
            message: `Remote server did not return a valid MCP response${status}`,
          });
          break;
        case "unreachable": {
          let suffix = "";
          if (response.httpStatus !== undefined) {
            suffix = ` (HTTP ${response.httpStatus})`;
          }
          setResult({
            verified: false,
            message: `${unreachableMessage(response.reason)}${suffix}`,
          });
          break;
        }
      }
      resultUrlRef.current = trimmed;
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to probe URL";
      if (latestUrlRef.current.trim() !== trimmed) return;
      setResult({ verified: false, message });
      resultUrlRef.current = trimmed;
    }
  };

  return { trigger, result, isPending: probe.isPending };
}
