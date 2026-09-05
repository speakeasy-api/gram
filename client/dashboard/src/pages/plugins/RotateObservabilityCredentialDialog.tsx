import { CodeBlock } from "@/components/code";
import { Alert, ErrorAlert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Label } from "@/components/ui/Label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/RadioGroup";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useFetcher } from "@/contexts/Fetcher";
import { invalidateAllListAPIKeys } from "@gram/client/react-query/listAPIKeys";
import { invalidateAllPublishStatus } from "@gram/client/react-query/publishStatus";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";

export type ObservabilityDownloadPlatform =
  | "claude"
  | "cursor"
  | "codex"
  | "opencode"
  | "openclaw";

export type PreviousKeyFate = "revoke_immediately" | "grace";

export type RotatedObservabilityKey = {
  id: string;
  name: string;
  key_prefix: string;
};

export type RotateObservabilityCredentialResult = {
  key?: string;
  key_prefix: string;
  previous_key_fate: PreviousKeyFate;
  previous_keys: RotatedObservabilityKey[];
  previous_keys_expire_at?: string;
  marketplace_republished: boolean;
  marketplace_update_deferred?: boolean;
};

const ZIP_PLATFORMS: {
  platform: ObservabilityDownloadPlatform;
  label: string;
}[] = [
  { platform: "claude", label: "Claude" },
  { platform: "cursor", label: "Cursor" },
  { platform: "codex", label: "Codex" },
  { platform: "opencode", label: "OpenCode" },
  { platform: "openclaw", label: "OpenClaw" },
];

function marketplaceStatusCopy(
  result: RotateObservabilityCredentialResult,
): string {
  if (result.marketplace_republished) {
    return "The published marketplace now embeds this credential. Installed copies pick it up on the next plugin update.";
  }
  if (result.marketplace_update_deferred) {
    return "A marketplace exists, but it was not updated yet because this organization is not cleared for the latest observability hooks. Existing marketplace installs keep the previous credential until the marketplace is republished.";
  }
  return "This project has no published marketplace. Use the key below for existing installs, or download a ZIP for a new package.";
}

function previousKeyFateCopy(
  result: RotateObservabilityCredentialResult,
): string {
  const count = result.previous_keys.length;
  if (count === 0) {
    return "No previous observability plugin keys were in use.";
  }
  if (result.previous_key_fate === "revoke_immediately") {
    return count === 1
      ? "The previous key was revoked immediately and no longer authenticates."
      : `${count} previous keys were revoked immediately and no longer authenticate.`;
  }
  const until = result.previous_keys_expire_at
    ? new Date(result.previous_keys_expire_at).toLocaleString()
    : "the end of the 7-day grace window";
  return count === 1
    ? `The previous key stays valid until ${until}.`
    : `${count} previous keys stay valid until ${until}.`;
}

async function readRPCError(response: Response): Promise<string> {
  try {
    const body = (await response.json()) as { message?: string };
    if (body.message) return body.message;
  } catch {
    // Fall through to the status-code message.
  }
  return `Rotation failed (${response.status})`;
}

export function RotateObservabilityCredentialDialog({
  open,
  onOpenChange,
  isDownloading,
  onDownload,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  isDownloading: boolean;
  onDownload: (platform: ObservabilityDownloadPlatform) => void;
}): JSX.Element {
  const { fetch: authFetch } = useFetcher();
  const queryClient = useQueryClient();
  const [fate, setFate] = useState<PreviousKeyFate>("grace");
  const [isPending, setIsPending] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const [result, setResult] = useState<RotateObservabilityCredentialResult | null>(
    null,
  );

  const reset = () => {
    setFate("grace");
    setIsPending(false);
    setError(null);
    setResult(null);
  };

  const handleOpenChange = (next: boolean) => {
    if (isPending) return;
    if (!next && result) return;
    if (!next) reset();
    onOpenChange(next);
  };

  const handleRotate = async () => {
    setIsPending(true);
    setError(null);
    try {
      const response = await authFetch(
        "/rpc/plugins.rotateObservabilityCredential",
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ previous_key_fate: fate }),
        },
      );
      if (!response.ok) {
        throw new Error(await readRPCError(response));
      }
      const body =
        (await response.json()) as RotateObservabilityCredentialResult;
      setResult(body);
      void invalidateAllListAPIKeys(queryClient);
      void invalidateAllPublishStatus(queryClient);
    } catch (err) {
      setError(
        err instanceof Error ? err : new Error("Failed to rotate credential"),
      );
    } finally {
      setIsPending(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <Dialog.Content
        closeable={!isPending && !result}
        className={
          result ? "max-h-[90vh] max-w-2xl overflow-y-auto" : undefined
        }
      >
        {result ? (
          <>
            <Dialog.Header>
              <Dialog.Title>Observability credential rotated</Dialog.Title>
              <Dialog.Description>
                Copy the new key now. Gram cannot show it again.
              </Dialog.Description>
            </Dialog.Header>
            <Stack gap={4}>
              <Alert variant="warning">
                This key is shown once. Store it securely before closing.
              </Alert>
              {result.key ? (
                <Stack gap={2}>
                  <Text as="h3" className="font-medium">
                    New hooks credential
                  </Text>
                  <CodeBlock copyLabel="observability credential">
                    {result.key}
                  </CodeBlock>
                </Stack>
              ) : null}
              <Alert variant="info">{previousKeyFateCopy(result)}</Alert>
              <Alert
                variant={
                  result.marketplace_update_deferred ? "warning" : "info"
                }
              >
                {marketplaceStatusCopy(result)}
              </Alert>
              <Stack gap={2}>
                <Text as="h3" className="font-medium">
                  Download an updated ZIP
                </Text>
                <Text muted small>
                  ZIP download mints a separate hooks key for that package.
                  Marketplace installs should use the republished package, and
                  existing installs should use the key above.
                </Text>
                <div className="flex flex-wrap gap-2">
                  {ZIP_PLATFORMS.map(({ platform, label }) => (
                    <Button
                      key={platform}
                      variant="secondary"
                      size="sm"
                      disabled={isDownloading}
                      onClick={() => {
                        onDownload(platform);
                      }}
                    >
                      <Button.Text>{label}</Button.Text>
                    </Button>
                  ))}
                </div>
              </Stack>
            </Stack>
            <Dialog.Footer>
              <Button
                onClick={() => {
                  reset();
                  onOpenChange(false);
                }}
              >
                I have saved the key
              </Button>
            </Dialog.Footer>
          </>
        ) : (
          <>
            <Dialog.Header>
              <Dialog.Title>Rotate observability credential</Dialog.Title>
              <Dialog.Description>
                Mint a replacement hooks-scoped API key for the Observability
                plugin. Choose what happens to keys already baked into installed
                copies.
              </Dialog.Description>
            </Dialog.Header>
            <RadioGroup
              value={fate}
              onValueChange={(value) => {
                setFate(value as PreviousKeyFate);
              }}
              className="gap-2"
            >
              <label
                htmlFor="observability-fate-grace"
                className="hover:bg-muted/40 flex cursor-pointer items-start gap-3 border px-3 py-2.5"
              >
                <RadioGroupItem
                  id="observability-fate-grace"
                  value="grace"
                  className="mt-0.5"
                />
                <div className="min-w-0">
                  <Label
                    htmlFor="observability-fate-grace"
                    className="cursor-pointer text-sm font-medium"
                  >
                    Keep the previous key valid for 7 days
                  </Label>
                  <Text muted small>
                    Installed copies keep working while you roll out the
                    replacement. The old key then stops authenticating.
                  </Text>
                </div>
              </label>
              <label
                htmlFor="observability-fate-revoke"
                className="hover:bg-muted/40 flex cursor-pointer items-start gap-3 border px-3 py-2.5"
              >
                <RadioGroupItem
                  id="observability-fate-revoke"
                  value="revoke_immediately"
                  className="mt-0.5"
                />
                <div className="min-w-0">
                  <Label
                    htmlFor="observability-fate-revoke"
                    className="cursor-pointer text-sm font-medium"
                  >
                    Revoke the previous key immediately
                  </Label>
                  <Text muted small>
                    Use this if the key may be leaked. Installed copies start
                    failing until they are updated.
                  </Text>
                </div>
              </label>
            </RadioGroup>
            {error ? <ErrorAlert error={error} /> : null}
            <Dialog.Footer>
              <Button
                variant="tertiary"
                onClick={() => {
                  handleOpenChange(false);
                }}
                disabled={isPending}
              >
                Cancel
              </Button>
              <Button
                variant="destructive-primary"
                onClick={() => {
                  void handleRotate();
                }}
                disabled={isPending}
              >
                {isPending ? "Rotating…" : "Rotate credential"}
              </Button>
            </Dialog.Footer>
          </>
        )}
      </Dialog.Content>
    </Dialog>
  );
}
