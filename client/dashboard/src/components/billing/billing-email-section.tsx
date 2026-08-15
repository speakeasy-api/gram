import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { type BillingEmail } from "@gram/client/models/components/billingemail.js";
import {
  invalidateAllGetBillingEmail,
  useGetBillingEmail,
} from "@gram/client/react-query/getBillingEmail.js";
import { useSetBillingEmailMutation } from "@gram/client/react-query/setBillingEmail.js";
import { useQueryClient } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";

// Who receives billing notifications for a pay-as-you-go organization. The
// scope gate wraps the component that owns the query so a member never fires
// an admin-only request.
export const BillingEmailSection = (): JSX.Element => (
  <RequireScope scope="org:admin" level="section">
    <BillingEmailSectionInner />
  </RequireScope>
);

function BillingEmailSectionInner(): JSX.Element {
  const { data, isError } = useGetBillingEmail();

  let body: JSX.Element;
  if (data) {
    body = <BillingEmailForm initial={data} />;
  } else if (isError) {
    body = (
      <Text muted small>
        Couldn't load the billing notification email. Reload the page to try
        again.
      </Text>
    );
  } else {
    body = (
      <div className="max-w-md space-y-4">
        <Skeleton className="h-9 w-full" />
        <Skeleton className="h-9 w-32" />
      </div>
    );
  }

  return (
    <Page.Section>
      {/* Secondary section below Usage: suppress the area eyebrow. */}
      <Page.Section.Title area="">Billing notifications</Page.Section.Title>
      <Page.Section.Description>
        Choose who receives billing notifications — invoices, payment failures,
        and usage alerts — for this organization.
      </Page.Section.Description>
      <Page.Section.Body>{body}</Page.Section.Body>
    </Page.Section>
  );
}

// The field seeds from the loaded value at mount and is never re-seeded, so a
// background refetch landing mid-edit can't overwrite what the admin typed.
function BillingEmailForm({ initial }: { initial: BillingEmail }): JSX.Element {
  const queryClient = useQueryClient();
  const [email, setEmail] = useState(() => initial.email ?? "");

  const mutation = useSetBillingEmailMutation({
    onSuccess: () => {
      void invalidateAllGetBillingEmail(queryClient);
    },
  });

  const handleChange = (value: string) => {
    // Whitespace is never part of an address, and the field's own validity is
    // what gates submission — so surrounding spaces come off here, or the
    // browser would reject a value the form would otherwise send as valid.
    setEmail(value.trim());
    // "Saved."/failure text left beside a field that has since been edited
    // reads as feedback about the value now in the field.
    if (mutation.isSuccess || mutation.isError) mutation.reset();
  };

  // Submission runs through the form so the email field's native validity
  // gates it: a malformed address is rejected in the field, where it can be
  // corrected, instead of coming back as a transient "try again" failure from
  // the API.
  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const trimmed = email.trim();
    mutation.mutate({
      request: {
        billingEmail: { email: trimmed === "" ? undefined : trimmed },
      },
    });
  };

  return (
    <form onSubmit={handleSubmit}>
      <Stack gap={4} className="max-w-md">
        <Stack gap={2}>
          <Label htmlFor="billing-notification-email">
            Billing notification email
          </Label>
          <Input
            id="billing-notification-email"
            type="email"
            placeholder="All org admins"
            value={email}
            onChange={handleChange}
          />
          <Text muted small>
            Leave this blank to send billing notifications to all organization
            admins. Enter an address to send them to that address only.
          </Text>
        </Stack>
        <Stack direction="horizontal" align="center" gap={3}>
          <Button type="submit" disabled={mutation.isPending}>
            {mutation.isPending ? "SAVING..." : "SAVE BILLING EMAIL"}
          </Button>
          {mutation.isSuccess && (
            <Text muted small role="status">
              Saved.
            </Text>
          )}
          {mutation.isError && (
            <Text small destructive role="alert">
              Couldn't save the billing notification email. Try again.
            </Text>
          )}
        </Stack>
      </Stack>
    </form>
  );
}
