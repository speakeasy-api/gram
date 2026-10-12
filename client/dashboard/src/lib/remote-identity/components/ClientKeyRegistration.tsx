import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Text } from "@/components/ui/Text";
import { useSdkClient } from "@/contexts/Sdk";
import { clientKeySetUrl } from "../model/serviceAccount";
import { Copy } from "lucide-react";
import { toast } from "sonner";

/**
 * How the provider learns the client's public key: the key set URL to paste
 * into the app, or the key itself for providers that only take a pasted key.
 */
export function ClientKeyRegistration({
  clientRowId,
  keySetId,
}: {
  clientRowId: string;
  keySetId: string;
}): JSX.Element {
  const client = useSdkClient();
  const url = clientKeySetUrl(clientRowId);

  // Only the keys a verifier should trust: pending keys are published so
  // caches warm, retired and revoked ones must not be registered again.
  const copyPublicKey = async () => {
    try {
      const result = await client.jsonWebKeySets.listKeys(
        { setId: keySetId },
        { sessionHeaderGramSession: "" },
      );
      const keys = result.keys
        .filter(
          (key) => key.keyState === "active" || key.keyState === "pending",
        )
        .map((key) => key.publicJwk as unknown);
      if (keys.length === 0) {
        toast.error("This key set has no published key yet.");
        return;
      }
      // Providers that take a pasted key want one JWK, not a set, so a single
      // published key is copied on its own.
      const copied = keys.length === 1 ? keys[0] : { keys };
      await navigator.clipboard.writeText(JSON.stringify(copied, null, 2));
      toast.success("Public key copied");
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "Couldn't copy the public key",
      );
    }
  };

  // Two ways to register the same key, laid out as matching rows so neither
  // reads as an afterthought: the URL for providers that fetch keys, the key
  // itself for providers that only take a pasted one.
  return (
    <div className="bg-card flex min-w-0 flex-col divide-y border">
      <div className="flex min-w-0 items-center gap-3 px-3 py-2.5">
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="text-eyebrow">Key set URL</span>
          {/* When the row is too narrow, hovering lays the whole URL over the
              truncated one rather than hiding it in a tooltip. The overlay is
              the text you see on hover, so one click selects the whole URL. */}
          <span className="group relative -mx-2 block min-w-0">
            <span className="text-muted-foreground block truncate px-2 py-0.5 font-mono text-xs">
              {url}
            </span>
            <span
              aria-hidden="true"
              className="bg-popover text-foreground absolute top-0 left-0 z-10 hidden cursor-text border px-2 py-0.5 font-mono text-xs whitespace-nowrap shadow-md select-all group-hover:block"
            >
              {url}
            </span>
          </span>
        </div>
        <CopyButton text={url} size="sm" tooltip="Copy key set URL" />
      </div>
      <div className="flex min-w-0 items-center gap-3 px-3 py-2.5">
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="text-eyebrow">Public key</span>
          <Text muted small>
            For providers that take a pasted key
          </Text>
        </div>
        <Button
          variant="tertiary"
          size="sm"
          onClick={() => void copyPublicKey()}
          aria-label="Copy public key"
          tooltip="Copy public key"
        >
          <Button.LeftIcon>
            <Copy />
          </Button.LeftIcon>
        </Button>
      </div>
    </div>
  );
}
