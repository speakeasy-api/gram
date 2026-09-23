import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Stack } from "@/components/ui/Stack";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { useState } from "react";

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

export function RegisterIssuerDialog({
  open,
  onOpenChange,
  onSubmit,
  isPending,
}: RegisterIssuerDialogProps): JSX.Element {
  const [values, setValues] = useState<RegisterIssuerValues>(EMPTY);

  const handleOpenChange = (next: boolean) => {
    if (!next) {
      setValues(EMPTY);
    }
    onOpenChange(next);
  };

  const canSubmit =
    values.name.trim().length > 0 &&
    values.issuer.trim().length > 0 &&
    values.jwksUri.trim().length > 0;

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
              onChange={(value) => setValues({ ...values, issuer: value })}
            />
            <Text muted small>
              The value an assertion&apos;s <code>iss</code> claim must carry.
              An https URL on a fully qualified domain, with no query or
              fragment.
            </Text>
          </Stack>

          <Stack gap={2}>
            <Label htmlFor="workload-issuer-jwks">JWKS URI</Label>
            <Input
              id="workload-issuer-jwks"
              value={values.jwksUri}
              placeholder="https://identity.example.com/.well-known/jwks.json"
              onChange={(value) => setValues({ ...values, jwksUri: value })}
            />
            <Text muted small>
              Where the issuer publishes its signing keys. This is the only
              field Gram reads when verifying an assertion.
            </Text>
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
              <Label id="workload-issuer-wildcard-label">
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
