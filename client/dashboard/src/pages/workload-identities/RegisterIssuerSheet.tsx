import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { Stack } from "@/components/ui/Stack";
import { TagInput } from "@/components/ui/TagInput";
import { Text } from "@/components/ui/Text";
import { TextArea } from "@/components/ui/Textarea";
import { useEffect, useState } from "react";
import { tagsProblem } from "./tagLimits";

export interface RegisterIssuerValues {
  name: string;
  description: string;
  issuer: string;
  jwksUri: string;
  tags: string[];
}

interface RegisterIssuerSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (values: RegisterIssuerValues) => void;
  isPending: boolean;
}

const EMPTY: RegisterIssuerValues = {
  name: "",
  description: "",
  issuer: "",
  jwksUri: "",
  tags: [],
};

// The column caps a description at 500 characters, as the server does.
const MAX_DESCRIPTION_LENGTH = 500;

function descriptionProblem(description: string): string | null {
  if (description.trim().length > MAX_DESCRIPTION_LENGTH) {
    return `At most ${MAX_DESCRIPTION_LENGTH} characters.`;
  }
  return null;
}

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

export function RegisterIssuerSheet({
  open,
  onOpenChange,
  onSubmit,
  isPending,
}: RegisterIssuerSheetProps): JSX.Element {
  const [values, setValues] = useState<RegisterIssuerValues>(EMPTY);

  // A successful registration closes the sheet through the parent's own state,
  // which never reaches handleOpenChange — so without this the next registration
  // opens prefilled with the previous issuer. The sheet stays mounted, so there
  // is no unmount to do it for us.
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
  const tagProblem = tagsProblem(values.tags);
  const descProblem = descriptionProblem(values.description);

  const canSubmit =
    values.name.trim().length > 0 &&
    values.issuer.trim().length > 0 &&
    values.jwksUri.trim().length > 0 &&
    issuerProblem === null &&
    jwksProblem === null &&
    descProblem === null &&
    tagProblem === null;

  const handleSubmit: React.FormEventHandler<HTMLFormElement> = (e) => {
    e.preventDefault();
    if (!canSubmit || isPending) return;
    onSubmit(values);
  };

  return (
    <Sheet open={open} onOpenChange={handleOpenChange}>
      <SheetContent
        side="right"
        className="flex w-[560px] max-w-[calc(100vw-2rem)] flex-col sm:max-w-[560px]"
      >
        <SheetHeader className="px-6 pt-6 pb-0">
          <SheetTitle className="text-lg font-semibold">
            Register new access
          </SheetTitle>
          <SheetDescription>
            Both values come from the platform issuing your machines&apos;
            tokens. Gram stores them exactly as entered, because an assertion is
            matched against the spelling you register.
          </SheetDescription>
        </SheetHeader>

        <form onSubmit={handleSubmit} className="flex min-h-0 flex-1 flex-col">
          <div className="flex-1 space-y-6 overflow-y-auto px-6 py-6">
            <Stack gap={2}>
              <Label htmlFor="workload-issuer-name">Name</Label>
              <Input
                id="workload-issuer-name"
                value={values.name}
                placeholder="New platform"
                onChange={(value) => setValues({ ...values, name: value })}
              />
              <Text muted small>
                How this issuer is labeled here. Not used for matching.
              </Text>
            </Stack>

            <Stack gap={2}>
              <Label htmlFor="workload-issuer-description">Description</Label>
              <TextArea
                id="workload-issuer-description"
                value={values.description}
                placeholder="Claude agents in our Slack workspace"
                rows={2}
                onChange={(value) =>
                  setValues({ ...values, description: value })
                }
              />
              {descProblem !== null ? (
                <Text
                  id="workload-issuer-description-error"
                  role="alert"
                  small
                  destructive
                >
                  {descProblem}
                </Text>
              ) : (
                <Text muted small>
                  Optional. What this platform is and what runs on it, shown in
                  place of the issuer URL in the Access Hub.
                </Text>
              )}
            </Stack>

            <Stack gap={2}>
              <Label htmlFor="workload-issuer-url">Issuer</Label>
              <Input
                id="workload-issuer-url"
                value={values.issuer}
                placeholder="https://identity.example.com"
                aria-invalid={issuerProblem !== null}
                aria-describedby={
                  issuerProblem !== null
                    ? "workload-issuer-url-error"
                    : undefined
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
                  The value an assertion&apos;s <code>iss</code> claim must
                  carry. An https URL on a fully qualified domain, with no query
                  or fragment.
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
                  jwksProblem !== null
                    ? "workload-issuer-jwks-error"
                    : undefined
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
              <Label htmlFor="workload-issuer-tags">Tags</Label>
              <TagInput
                id="workload-issuer-tags"
                value={values.tags}
                placeholder="production, ci"
                error={tagProblem !== null}
                ariaDescribedBy={
                  tagProblem !== null ? "workload-issuer-tags-error" : undefined
                }
                onChange={(tags) => setValues({ ...values, tags })}
              />
              {tagProblem !== null ? (
                <Text
                  id="workload-issuer-tags-error"
                  role="alert"
                  small
                  destructive
                >
                  {tagProblem}
                </Text>
              ) : (
                <Text muted small>
                  Optional labels for grouping platforms here. Not used for
                  matching.
                </Text>
              )}
            </Stack>
          </div>

          <SheetFooter className="flex-row items-center justify-end gap-2 border-t px-6 py-4">
            <Button
              type="button"
              variant="secondary"
              onClick={() => handleOpenChange(false)}
              disabled={isPending}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
            <Button
              type="submit"
              variant="primary"
              disabled={!canSubmit || isPending}
            >
              <Button.Text>
                {isPending ? "Registering…" : "Register"}
              </Button.Text>
            </Button>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  );
}
