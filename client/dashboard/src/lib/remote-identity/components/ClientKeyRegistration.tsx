import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Text } from "@/components/ui/Text";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildListJsonWebKeysQuery } from "@gram/client/react-query/listJsonWebKeys.js";
import { useQueryClient } from "@tanstack/react-query";
import { clientKeySetUrl } from "../model/serviceAccount";
import { Copy } from "lucide-react";
import { toast } from "sonner";

const SECURITY = { sessionHeaderGramSession: "" };

/**
 * How the provider learns the client's public keys: the key set URL to paste
 * into the app, or the keys themselves for providers that only take a pasted
 * key set.
 */
export function ClientKeyRegistration({
  clientRowId,
  keySetId,
}: {
  clientRowId: string;
  keySetId: string;
}): JSX.Element {
  const gramClient = useGramContext();
  const queryClient = useQueryClient();
  const url = clientKeySetUrl(clientRowId);

  // Always asks the server: cached keys can predate a rotation, and copying
  // a stale set would register a key the provider then can't verify with.
  // Copies every key a verifier should trust, as a set even when there is
  // one: the key that signs now and the one about to, which is what the key
  // set URL serves. Retired and revoked keys must not be registered again.
  const fetchKeySetText = async (): Promise<string> => {
    const { keys } = await queryClient.fetchQuery({
      ...buildListJsonWebKeysQuery(gramClient, { setId: keySetId }, SECURITY),
      staleTime: 0,
    });
    const published = keys
      .filter((key) => key.keyState === "active" || key.keyState === "pending")
      .map((key) => key.publicJwk);
    if (published.length === 0) {
      throw new Error("This key set has no published key yet.");
    }
    return JSON.stringify({ keys: published }, null, 2);
  };

  const copyPublicKey = async () => {
    try {
      const text = fetchKeySetText();
      if (typeof ClipboardItem !== "undefined") {
        // Hand the clipboard a promise within the click: Safari refuses a
        // clipboard write that follows an await, so the write has to start
        // now and resolve once the keys arrive.
        const blob = text.then(
          (value) => new Blob([value], { type: "text/plain" }),
        );
        try {
          await navigator.clipboard.write([
            new ClipboardItem({ "text/plain": blob }),
          ]);
        } catch (error) {
          // The browser reports a failed promise as its own error; the
          // lookup's reason says more.
          await text;
          throw error;
        }
      } else {
        await navigator.clipboard.writeText(await text);
      }
      toast.success("Public key copied");
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : "Couldn't copy the public key",
      );
    }
  };

  // Two ways to register the same key, laid out as matching rows so neither
  // reads as an afterthought: the URL for providers that fetch keys, the keys
  // themselves for providers that only take a pasted set.
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
            For providers that take a pasted key set
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
