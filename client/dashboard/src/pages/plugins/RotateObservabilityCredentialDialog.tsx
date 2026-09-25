import { CodeBlock } from "@/components/code";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import type { ObservabilityDownloadPlatform } from "./observability-platforms";
import { OBSERVABILITY_DOWNLOAD_PLATFORMS } from "./observability-platforms";
import type { RotateObservabilityCredentialResult } from "@gram/client/models/components/rotateobservabilitycredentialresult.js";
import { invalidateAllListAPIKeys } from "@gram/client/react-query/listAPIKeys";
import { invalidateAllPublishStatus } from "@gram/client/react-query/publishStatus";
import { useRotateObservabilityCredentialMutation } from "@gram/client/react-query/rotateObservabilityCredential";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";

type PreviousKeyFate = "revoke_immediately" | "grace";

const GRACE_DAYS = 7;

function marketplaceStatusCopy(
  result: RotateObservabilityCredentialResult,
): string {
  if (result.marketplaceRepublished) {
    return "The published marketplace now embeds this credential. Installed copies pick it up on the next plugin update.";
  }
  if (result.marketplaceUpdateDeferred) {
    return "A marketplace exists, but it could not be updated yet — this organization is not cleared for the latest observability hooks. Existing marketplace installs keep the previous credential until the marketplace is republished.";
  }
  return "This project has no published marketplace. Use the key above for existing installs, or download a ZIP for a new package.";
}

function previousKeyFateCopy(
  result: RotateObservabilityCredentialResult,
): string {
  const count = result.previousKeys.length;
  if (count === 0) {
    return "No previous observability plugin keys were in use.";
  }
  if (result.previousKeyFate === "revoke_immediately") {
    return count === 1
      ? "The previous key was revoked immediately and no longer authenticates."
      : `${count} previous keys were revoked immediately and no longer authenticate.`;
  }
  const until = result.previousKeysExpireAt
    ? result.previousKeysExpireAt.toLocaleString()
    : `the end of the ${GRACE_DAYS}-day grace window`;

  return count === 1
    ? `The previous key stays valid until ${until}.`
    : `${count} previous keys stay valid until ${until}.`;
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
  const queryClient = useQueryClient();
  const [fate, setFate] = useState<PreviousKeyFate>("grace");
  const [result, setResult] =
    useState<RotateObservabilityCredentialResult | null>(null);

  const rotateMutation = useRotateObservabilityCredentialMutation({
    onError: () => {
      toast.error("Failed to rotate the observability credential");
    },
  });

  const reset = () => {
    setFate("grace");
    setResult(null);
  };

  const close = () => {
    reset();
    onOpenChange(false);
  };

  // The plaintext key is shown once, so a stray outside-click or Escape must not
  // take it away; only the explicit acknowledgement closes the result step.
  const handleOpenChange = (next: boolean) => {
    if (next) {
      onOpenChange(true);
      return;
    }
    if (rotateMutation.isPending || result) return;
    close();
  };

  const handleRotate = () => {
    rotateMutation.mutate(
      {
        security: { sessionHeaderGramSession: "" },
        request: {
          rotateObservabilityCredentialRequestBody: { previousKeyFate: fate },
        },
      },
      {
        onSuccess: async (data) => {
          setResult(data);
          await Promise.all([
            invalidateAllListAPIKeys(queryClient),
            invalidateAllPublishStatus(queryClient),
          ]);
        },
      },
    );
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <Dialog.Content
        closeable={!rotateMutation.isPending && !result}
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
              <Stack gap={2}>
                <Text as="h3" className="font-medium">
                  New hooks credential
                </Text>
                <CodeBlock copyLabel="observability credential">
                  {result.key}
                </CodeBlock>
              </Stack>
              <Alert variant="info">{previousKeyFateCopy(result)}</Alert>
              <Alert
                variant={result.marketplaceUpdateDeferred ? "warning" : "info"}
              >
                {marketplaceStatusCopy(result)}
              </Alert>
              <Stack gap={2}>
                <Text as="h3" className="font-medium">
                  Download an updated ZIP
                </Text>
                <Text muted small>
                  A ZIP download mints its own hooks key for that package.
                  Marketplace installs should take the republished package, and
                  existing installs the key above.
                </Text>
                <div className="flex flex-wrap gap-2">
                  {OBSERVABILITY_DOWNLOAD_PLATFORMS.map(
                    ({ platform, label }) => (
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
                    ),
                  )}
                </div>
              </Stack>
            </Stack>
            <Dialog.Footer>
              <Button onClick={close}>
                <Button.Text>I have saved the key</Button.Text>
              </Button>
            </Dialog.Footer>
          </>
        ) : (
          <>
            <Dialog.Header>
              <Dialog.Title>Rotate observability credential</Dialog.Title>
              <Dialog.Description>
                Mint a replacement hooks-scoped API key for the Observability
                plugin. Choose what happens to the key already baked into
                installed copies.
              </Dialog.Description>
            </Dialog.Header>
            <RadioCardGroup
              value={fate}
              onValueChange={(value) => {
                setFate(value as PreviousKeyFate);
              }}
              aria-label="What happens to the previous key"
            >
              <RadioCard
                value="grace"
                title={`Keep the previous key valid for ${GRACE_DAYS} days`}
              >
                <Text muted small>
                  Installed copies keep reporting while you roll out the
                  replacement. The old key then stops authenticating.
                </Text>
              </RadioCard>
              <RadioCard
                value="revoke_immediately"
                title="Revoke the previous key immediately"
              >
                <Text muted small>
                  Use this if the key may be leaked. Installed copies stop
                  reporting until they are updated.
                </Text>
              </RadioCard>
            </RadioCardGroup>
            <Dialog.Footer>
              <Button
                variant="tertiary"
                onClick={close}
                disabled={rotateMutation.isPending}
              >
                <Button.Text>Cancel</Button.Text>
              </Button>
              <Button
                variant="destructive-primary"
                onClick={handleRotate}
                disabled={rotateMutation.isPending}
              >
                <Button.Text>
                  {rotateMutation.isPending ? "Rotating…" : "Rotate credential"}
                </Button.Text>
              </Button>
            </Dialog.Footer>
          </>
        )}
      </Dialog.Content>
    </Dialog>
  );
}
