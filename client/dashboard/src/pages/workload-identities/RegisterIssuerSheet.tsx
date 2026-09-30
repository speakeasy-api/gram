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
import { issuerValuesDiffer } from "./issuerEdit";
import { httpsUrlProblem } from "./issuerUrl";
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
  /**
   * A registered issuer's current values. When set, the sheet edits that
   * issuer: the form opens prefilled and the issuer URL is read-only, because
   * the server fixes it at registration.
   */
  initial?: RegisterIssuerValues;
}

const EMPTY: RegisterIssuerValues = {
  name: "",
  description: "",
  issuer: "",
  jwksUri: "",
  tags: [],
};

// The column caps a name at 100 characters, as the server does.
const MAX_NAME_LENGTH = 100;

function nameProblem(name: string): string | null {
  if (Array.from(name.trim()).length > MAX_NAME_LENGTH) {
    return `At most ${MAX_NAME_LENGTH} characters.`;
  }
  return null;
}

// The column caps a description at 500 characters, as the server does.
const MAX_DESCRIPTION_LENGTH = 500;

function descriptionProblem(description: string): string | null {
  // Code points, as the server counts them, not UTF-16 units.
  if (Array.from(description.trim()).length > MAX_DESCRIPTION_LENGTH) {
    return `At most ${MAX_DESCRIPTION_LENGTH} characters.`;
  }
  return null;
}

function submitLabel(isEditing: boolean, isPending: boolean): string {
  if (isEditing) {
    return isPending ? "Saving…" : "Save changes";
  }
  return isPending ? "Registering…" : "Register";
}

function IssuerUrlHint({ isEditing }: { isEditing: boolean }): JSX.Element {
  if (isEditing) {
    return (
      <Text muted small>
        Fixed at registration. To trust a different issuer, register it as a new
        platform.
      </Text>
    );
  }
  return (
    <Text muted small>
      The value an assertion&apos;s <code>iss</code> claim must carry. An https
      URL on a fully qualified domain, with no query or fragment.
    </Text>
  );
}

export function RegisterIssuerSheet({
  open,
  onOpenChange,
  onSubmit,
  isPending,
  initial,
}: RegisterIssuerSheetProps): JSX.Element {
  const isEditing = initial !== undefined;
  const resetTo = initial ?? EMPTY;
  const [values, setValues] = useState<RegisterIssuerValues>(resetTo);

  // A successful submit closes the sheet through the parent's own state, which
  // never reaches handleOpenChange — so without this the next registration opens
  // prefilled with the previous issuer, and the next edit with values that were
  // never saved. The sheet stays mounted, so there is no unmount to do it for us.
  useEffect(() => {
    if (!open) {
      setValues(initial ?? EMPTY);
    }
  }, [open, initial]);

  const handleOpenChange = (next: boolean) => {
    if (!next) {
      setValues(resetTo);
    }
    onOpenChange(next);
  };

  const issuerProblem = httpsUrlProblem(values.issuer, true);
  const jwksProblem = httpsUrlProblem(values.jwksUri, false);
  const tagProblem = tagsProblem(values.tags);
  const descProblem = descriptionProblem(values.description);

  const nameError = nameProblem(values.name);

  const hasChanges =
    initial === undefined || issuerValuesDiffer(initial, values);

  const canSubmit =
    hasChanges &&
    values.name.trim().length > 0 &&
    nameError === null &&
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
            {isEditing ? "Edit platform" : "Register new access"}
          </SheetTitle>
          <SheetDescription>
            {isEditing
              ? "The issuer URL is fixed once a platform is registered. Changing the JWKS URI changes which keys Gram accepts assertions from."
              : "Both values come from the platform issuing your machines' tokens. Gram trims surrounding spaces and otherwise stores them exactly as entered, because an assertion is matched against the spelling you register."}
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
                aria-invalid={nameError !== null}
                aria-describedby={
                  nameError !== null ? "workload-issuer-name-error" : undefined
                }
                onChange={(value) => setValues({ ...values, name: value })}
              />
              {nameError !== null ? (
                <Text
                  id="workload-issuer-name-error"
                  role="alert"
                  small
                  destructive
                >
                  {nameError}
                </Text>
              ) : (
                <Text muted small>
                  How this issuer is labeled here. Not used for matching.
                </Text>
              )}
            </Stack>

            <Stack gap={2}>
              <Label htmlFor="workload-issuer-description">Description</Label>
              <TextArea
                id="workload-issuer-description"
                value={values.description}
                placeholder="Claude agents in our Slack workspace"
                rows={2}
                aria-invalid={descProblem !== null || undefined}
                aria-describedby={
                  descProblem !== null
                    ? "workload-issuer-description-error"
                    : undefined
                }
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
                readOnly={isEditing}
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
                <IssuerUrlHint isEditing={isEditing} />
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
              <Button.Text>{submitLabel(isEditing, isPending)}</Button.Text>
            </Button>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  );
}
