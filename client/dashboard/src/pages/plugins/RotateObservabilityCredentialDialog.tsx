import { CodeBlock } from "@/components/code";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import type { ObservabilityDownloadPlatform } from "./observability-platforms";
import { OBSERVABILITY_DOWNLOAD_PLATFORMS } from "./observability-platforms";
import { rotationErrorCopy } from "./rotation-error-copy";
import type { RotateObservabilityCredentialResult } from "@gram/client/models/components/rotateobservabilitycredentialresult.js";
import { invalidateAllListAPIKeys } from "@gram/client/react-query/listAPIKeys";
import { invalidateAllPublishStatus } from "@gram/client/react-query/publishStatus";
import { useRotateObservabilityCredentialMutation } from "@gram/client/react-query/rotateObservabilityCredential";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";

type PreviousKeyFate = "revoke_immediately" | "grace";

const GRACE_DAYS = 7;

/**
 * What happened to the marketplace package, without guessing why. A deferred
 * update can mean this organization is not yet on the latest observability
 * plugin or that publishing is unavailable, and the customer's next step is
 * the same either way.
 */
function marketplaceStatusCopy(
  result: RotateObservabilityCredentialResult,
): string {
  if (result.marketplaceRepublished) {
    return "Your marketplace package now carries the new key. Installations pick it up the next time they update the plugin.";
  }
  if (result.marketplaceUpdateDeferred) {
    return "Your marketplace package was not updated, so it still carries the previous key. Installations that pull from it keep using the previous key until the package is published again — use the key above to update them in the meantime.";
  }
  return "This project has no marketplace package. Use the key above to update existing installations, or download a package below.";
}

function formatDeadline(value: Date): string {
  return value.toLocaleString();
}

/**
 * Deadlines are read back per key rather than assumed, because a key already
 * inside a shorter window keeps its earlier deadline instead of being extended
 * by this rotation.
 */
function previousKeyFateCopy(
  result: RotateObservabilityCredentialResult,
): string {
  const count = result.previousKeys.length;
  if (count === 0) {
    return "No previous observability keys were in use.";
  }
  if (result.previousKeyFate === "revoke_immediately") {
    const subject =
      count === 1 ? "The previous key" : `All ${count} previous keys`;
    return `${subject} stopped working. Installations still using ${count === 1 ? "it" : "them"} cannot send observability data until you give them the new key.`;
  }

  const deadlines = result.previousKeys
    .map((key) => key.expiresAt)
    .filter((value): value is Date => value !== undefined)
    .map((value) => value.getTime());

  if (deadlines.length === 0) {
    return count === 1
      ? `The previous key keeps working for up to ${GRACE_DAYS} days.`
      : `${count} previous keys keep working for up to ${GRACE_DAYS} days.`;
  }

  const latest = formatDeadline(new Date(Math.max(...deadlines)));
  const earliest = Math.min(...deadlines);
  const subject =
    count === 1 ? "The previous key keeps" : `${count} previous keys keep`;

  if (earliest === Math.max(...deadlines)) {
    return `${subject} working until ${latest}.`;
  }
  return `${subject} working until ${latest} at the latest. Some stop earlier — the first on ${formatDeadline(new Date(earliest))}.`;
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
    onError: (error) => {
      toast.error(rotationErrorCopy(error));
    },
  });

  const reset = () => {
    setFate("grace");
    setResult(null);
    // The mutation cache holds the same plaintext key the dialog just revealed,
    // so dropping local state alone would leave it recoverable after the
    // one-time reveal is dismissed.
    rotateMutation.reset();
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
        onSuccess: (data) => {
          setResult(data);
          // The reveal does not wait on the refetches; they only refresh the
          // Keys list and publish status behind the dialog.
          void invalidateAllListAPIKeys(queryClient);
          void invalidateAllPublishStatus(queryClient);
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
                Copy the new key now. Speakeasy cannot show it again.
              </Dialog.Description>
            </Dialog.Header>
            <Stack gap={4}>
              <Stack gap={2}>
                <Text as="h3" className="font-medium">
                  New observability key
                </Text>
                <CodeBlock copyLabel="observability credential">
                  {result.key}
                </CodeBlock>
              </Stack>
              {/*
                A partial rotation reports no previous keys, because none were
                retired — so the fate alert would read "no previous keys were in
                use", contradicting the warning. The warning is the whole story
                on that path.
              */}
              {result.previousKeysRetired ? (
                <Alert
                  variant={
                    result.previousKeyFate === "revoke_immediately" &&
                    result.previousKeys.length > 0
                      ? "warning"
                      : "info"
                  }
                >
                  {previousKeyFateCopy(result)}
                </Alert>
              ) : (
                <Alert variant="warning">
                  The new key works, but the previous keys were left untouched —
                  they can still send data. Rotate again to retire them.
                </Alert>
              )}
              <Alert
                variant={result.marketplaceUpdateDeferred ? "warning" : "info"}
              >
                {marketplaceStatusCopy(result)}
              </Alert>
              <Stack gap={2}>
                <Text as="h3" className="font-medium">
                  Download an updated package
                </Text>
                <Text muted small>
                  A download comes with its own key for that package. Use the
                  key above for installations you are updating by hand.
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
                Create a new key for sending observability data to Speakeasy,
                and choose how long existing installations can keep using their
                current key. You will see the new key once — replace it in your
                marketplace package and anywhere the plugin was installed by
                hand.
              </Dialog.Description>
            </Dialog.Header>
            <RadioCardGroup
              value={fate}
              onValueChange={(value) => {
                setFate(value as PreviousKeyFate);
              }}
              aria-label="What happens to the current key"
            >
              <RadioCard
                value="grace"
                title={`Let existing installations keep using the current key for ${GRACE_DAYS} days`}
              >
                <Text muted small>
                  They keep sending data while you roll out the new key. The
                  current key then stops working.
                </Text>
              </RadioCard>
              <RadioCard
                value="revoke_immediately"
                title="Stop the current key from working now"
              >
                <Text muted small>
                  Use this if the key may have leaked. Installations stop
                  sending data until you give them the new key.
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
