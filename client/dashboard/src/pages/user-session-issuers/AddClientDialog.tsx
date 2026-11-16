import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { FieldError } from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Text } from "@/components/ui/Text";
import type { VerifyCimdURLResult } from "@gram/client/models/components/verifycimdurlresult.js";
import { useVerifyUserSessionIssuerCimdClientURLMutation } from "@gram/client/react-query/verifyUserSessionIssuerCimdClientURL.js";
import { useId, useState } from "react";
import { monogram, summarizeDocument } from "./knownClients";
import type { OrganizationClients } from "./useOrganizationClients";

/**
 * Allows one more client document on a provider. Checking it first runs the
 * same fetch and validation sign-in will, so the person sees which client it
 * is and where it sends people before allowing it. The check runs through a
 * project, so a page outside one adds without it.
 */
export function AddClientDialog({
  open,
  onOpenChange,
  clients,
  canCheck,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  clients: OrganizationClients;
  canCheck: boolean;
}): JSX.Element {
  const fieldId = useId();
  const [url, setUrl] = useState("");
  const [checked, setChecked] = useState<{
    url: string;
    result: VerifyCimdURLResult;
  } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const verify = useVerifyUserSessionIssuerCimdClientURLMutation({
    onSuccess: (result) => setChecked({ url: url.trim(), result }),
    onError: (err) =>
      setError(err instanceof Error ? err.message : "Couldn't check it."),
  });

  const trimmed = url.trim();
  // A check covers the address it ran on, not one edited since.
  const result = checked?.url === trimmed ? checked.result : null;
  const summary = summarizeDocument(result?.document);
  const duplicate = (clients.items ?? []).some(
    (client) => client.clientIdMetadataUri === trimmed,
  );
  const canAdd =
    trimmed !== "" &&
    !duplicate &&
    !clients.adding &&
    (canCheck ? result?.verified === true : true);

  const reset = (): void => {
    setUrl("");
    setChecked(null);
    setError(null);
  };

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!next) reset();
        onOpenChange(next);
      }}
    >
      <Dialog.Content className="max-w-xl">
        <Dialog.Header>
          <Dialog.Title>Add a client</Dialog.Title>
          <Dialog.Description>
            {canCheck
              ? "Paste the address of the client's published document. Speakeasy checks it before you add it."
              : "Paste the address of the client's published document."}
          </Dialog.Description>
        </Dialog.Header>

        <div className="flex flex-col gap-2">
          <Label htmlFor={`${fieldId}-url`}>Document address</Label>
          <div className="flex gap-2">
            <Input
              id={`${fieldId}-url`}
              type="url"
              value={url}
              onChange={(value: string) => {
                setUrl(value);
                setError(null);
              }}
              placeholder="https://example.com/oauth/client-metadata.json"
              className="min-w-0 flex-1 font-mono text-xs"
            />
            {canCheck && (
              <Button
                variant="secondary"
                size="md"
                disabled={trimmed === "" || verify.isPending}
                onClick={() => {
                  setError(null);
                  verify.mutate({
                    request: {
                      verifyURLRequestBody: { clientIdMetadataUri: trimmed },
                    },
                  });
                }}
              >
                <Button.Text>
                  {verify.isPending ? "Checking" : "Check"}
                </Button.Text>
              </Button>
            )}
          </div>
          {duplicate && (
            <FieldError>This client is already allowed here.</FieldError>
          )}
          {error && <FieldError>{error}</FieldError>}
          {clients.addError && (
            <FieldError>{clients.addError.message}</FieldError>
          )}
        </div>

        {result && !result.verified && <FieldError>{result.detail}</FieldError>}

        {result?.verified && (
          <div className="flex flex-col gap-3 border p-4">
            <div className="flex items-center gap-3">
              <span
                aria-hidden="true"
                className="bg-muted flex size-10 shrink-0 items-center justify-center border font-mono text-xs"
              >
                {monogram(result.clientName ?? "?")}
              </span>
              <div className="flex min-w-0 flex-col">
                <Text>{result.clientName ?? "Unnamed client"}</Text>
                <Text small muted>
                  The document is valid and reachable.
                </Text>
              </div>
            </div>
            <dl className="grid grid-cols-[140px_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">
              <dt className="text-muted-foreground">Callback URL</dt>
              <dd className="font-mono text-xs break-all whitespace-pre-line">
                {summary.callbackUrls.length > 0
                  ? summary.callbackUrls.join("\n")
                  : "None listed"}
              </dd>
              {summary.website && (
                <>
                  <dt className="text-muted-foreground">Website</dt>
                  <dd className="font-mono text-xs break-all">
                    {summary.website}
                  </dd>
                </>
              )}
            </dl>
          </div>
        )}

        <Text small muted>
          Only this MCP Authentication Provider allows it. People still approve
          it on the consent screen.
        </Text>

        <Dialog.Footer>
          <Button variant="tertiary" onClick={() => onOpenChange(false)}>
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            variant="primary"
            disabled={!canAdd}
            onClick={() =>
              clients.add(trimmed, () => {
                reset();
                onOpenChange(false);
              })
            }
          >
            <Button.Text>
              {clients.adding ? "Adding" : "Add client"}
            </Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
