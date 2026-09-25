import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Stack } from "@/components/ui/Stack";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { useEffect, useState } from "react";

export interface RegisterIssuerValues {
  name: string;
  issuer: string;
  jwksUri: string;
  allowWildcardAdmission: boolean;
}

interface RegisterIssuerDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (values: RegisterIssuerValues) => void;
  isPending: boolean;
}

const EMPTY: RegisterIssuerValues = {
  name: "",
  issuer: "",
  jwksUri: "",
  allowWildcardAdmission: false,
};

// Mirrors what the server refuses on the write path, so the reason appears next
// to the field instead of arriving as a toast after submit. Deliberately not a
// full URL validator: the server stays the authority, this is the early warning.
function httpsUrlProblem(raw: string, isIssuer: boolean): string | null {
  const trimmed = raw.trim();
  if (trimmed.length === 0) {
    return null;
  }

  let parsed: URL;
  try {
    parsed = new URL(trimmed);
  } catch {
    return "Enter a complete URL, including https://.";
  }

  if (parsed.protocol !== "https:") {
    return "Must use https. Gram fetches the signing keys over this URL, so http would put key retrieval in the clear.";
  }
  const host = parsed.hostname.replace(/\.$/, "");
  if (!host.includes(".") || /^[\d.]+$/.test(host)) {
    return "Must name a fully qualified domain, not an IP address or a single-label host.";
  }
  if (isIssuer && (parsed.search !== "" || parsed.hash !== "")) {
    return "An issuer identifier carries no query string or fragment.";
  }

  return null;
}

export function RegisterIssuerDialog({
  open,
  onOpenChange,
  onSubmit,
  isPending,
}: RegisterIssuerDialogProps): JSX.Element {
  const [values, setValues] = useState<RegisterIssuerValues>(EMPTY);

  // A successful registration closes the dialog through the parent's own state,
  // which never reaches handleOpenChange — so without this the next registration
  // opens prefilled with the previous issuer. The dialog stays mounted, so there
  // is no unmount to do it for us. Same reason as AdmitSubjectDialog.
  useEffect(() => {
    if (!open) {
      setValues(EMPTY);
    }
  }, [open]);

  const handleOpenChange = (next: boolean) => {
    if (!next) {
      setValues(EMPTY);
    }
    onOpenChange(next);
  };

  const issuerProblem = httpsUrlProblem(values.issuer, true);
  const jwksProblem = httpsUrlProblem(values.jwksUri, false);

  const canSubmit =
    values.name.trim().length > 0 &&
    values.issuer.trim().length > 0 &&
    values.jwksUri.trim().length > 0 &&
    issuerProblem === null &&
    jwksProblem === null;

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Trust a workload issuer</Dialog.Title>
          <Dialog.Description>
            Both values come from the platform issuing your workloads&apos;
            tokens. Gram stores them exactly as entered, because an assertion is
            matched against the spelling you register.
          </Dialog.Description>
        </Dialog.Header>

        <Stack gap={4}>
          <Stack gap={2}>
            <Label htmlFor="workload-issuer-name">Name</Label>
            <Input
              id="workload-issuer-name"
              value={values.name}
              placeholder="Claude Tag"
              onChange={(value) => setValues({ ...values, name: value })}
            />
            <Text muted small>
              How this issuer is labeled here. Not used for matching.
            </Text>
          </Stack>

          <Stack gap={2}>
            <Label htmlFor="workload-issuer-url">Issuer</Label>
            <Input
              id="workload-issuer-url"
              value={values.issuer}
              placeholder="https://identity.example.com"
              aria-invalid={issuerProblem !== null}
              aria-describedby={
                issuerProblem !== null ? "workload-issuer-url-error" : undefined
              }
              onChange={(value) => setValues({ ...values, issuer: value })}
            />
            {issuerProblem !== null ? (
              <Text
                id="workload-issuer-url-error"
                role="alert"
                small
                destructive
              >
                {issuerProblem}
              </Text>
            ) : (
              <Text muted small>
                The value an assertion&apos;s <code>iss</code> claim must carry.
                An https URL on a fully qualified domain, with no query or
                fragment.
              </Text>
            )}
          </Stack>

          <Stack gap={2}>
            <Label htmlFor="workload-issuer-jwks">JWKS URI</Label>
            <Input
              id="workload-issuer-jwks"
              value={values.jwksUri}
              placeholder="https://identity.example.com/.well-known/jwks.json"
              aria-invalid={jwksProblem !== null}
              aria-describedby={
                jwksProblem !== null ? "workload-issuer-jwks-error" : undefined
              }
              onChange={(value) => setValues({ ...values, jwksUri: value })}
            />
            {jwksProblem !== null ? (
              <Text
                id="workload-issuer-jwks-error"
                role="alert"
                small
                destructive
              >
                {jwksProblem}
              </Text>
            ) : (
              <Text muted small>
                Where the issuer publishes its signing keys. This is the only
                field Gram reads when verifying an assertion.
              </Text>
            )}
          </Stack>

          <Stack gap={2}>
            <Stack direction="horizontal" align="center" gap={3}>
              <Switch
                aria-labelledby="workload-issuer-wildcard-label"
                checked={values.allowWildcardAdmission}
                onCheckedChange={(checked) =>
                  setValues({ ...values, allowWildcardAdmission: checked })
                }
              />
              <Label
                id="workload-issuer-wildcard-label"
                className="cursor-pointer"
                onClick={() =>
                  setValues({
                    ...values,
                    allowWildcardAdmission: !values.allowWildcardAdmission,
                  })
                }
              >
                Allow wildcard admission
              </Label>
            </Stack>
            <Text muted small>
              Only turn this on where the varying part of a subject is minted by
              the issuer and cannot be influenced by the caller. On a platform
              that puts a branch name in the subject, a wildcard admits anyone
              who can push a branch. Turning it back off makes existing wildcard
              rules inert immediately.
            </Text>
          </Stack>
        </Stack>

        <Dialog.Footer>
          <Button
            variant="tertiary"
            onClick={() => handleOpenChange(false)}
            disabled={isPending}
          >
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            onClick={() => onSubmit(values)}
            disabled={!canSubmit || isPending}
          >
            <Button.Text>
              {isPending ? "Trusting…" : "Trust issuer"}
            </Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
