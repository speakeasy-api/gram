import { useVerifyRemoteMcpURLMutation } from "@gram/client/react-query/index.js";
import { useEffect, useRef, useState } from "react";

export type VerifyResult = {
  verified: boolean;
  message: string;
};

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
  const verify = useVerifyRemoteMcpURLMutation();
  const [result, setResult] = useState<VerifyResult | null>(null);
  const resultUrlRef = useRef<string | null>(null);

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
      const response = await verify.mutateAsync({
        request: {
          verifyURLForm: {
            url: trimmed,
            transportType: "streamable-http",
          },
        },
      });
      setResult({ verified: response.verified, message: response.message });
      resultUrlRef.current = trimmed;
    } catch (error) {
      const message =
        error instanceof Error ? error.message : "Failed to verify URL";
      setResult({ verified: false, message });
      resultUrlRef.current = trimmed;
    }
  };

  return { trigger, result, isPending: verify.isPending };
}
