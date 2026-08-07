import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Skeleton } from "@/components/ui/Skeleton";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { type TokensUnderManagement } from "@gram/client/models/components/tokensundermanagement.js";
import {
  invalidateAllGetTokensUnderManagement,
  useGetTokensUnderManagement,
} from "@gram/client/react-query/getTokensUnderManagement.js";
import { useSetBillingMetadataMutation } from "@gram/client/react-query/setBillingMetadata.js";
import { useQueryClient } from "@tanstack/react-query";
import React, { Suspense, useMemo, useState } from "react";
import { ErrorBoundary } from "react-error-boundary";
import { handleError, toError } from "@/lib/errors";
import { cyclesFromTum } from "./billing-cycles";

// Split out of the initial bundle. The contract terms are what an admin comes
// to this section to change; the estimate is a read-only aside built entirely
// from data already on the page. Loading it lazily keeps the pricing module
// and its arithmetic off the billing page's first paint — the form is
// interactive before the estimator's chunk has even been fetched.
const ContractPriceEstimator = React.lazy(() =>
  import("./contract-price-estimator").then((mod) => ({
    default: mod.ContractPriceEstimator,
  })),
);

// Platform-admin form for the org's contracted tokens-under-management
// terms: monthly allowance, alert email, and the billing cycle anchor day.
// The form only mounts once the current terms have loaded — its fields seed
// from them at mount, so saving-before-load (which would overwrite terms
// with empty defaults) is unrepresentable, and background refetches can't
// clobber in-progress edits.
export const TumAdminSection = (): JSX.Element => {
  const { data: tum, isError } = useGetTokensUnderManagement();

  let body: JSX.Element;
  if (tum) {
    body = <ContractForm initial={tum} />;
  } else if (isError) {
    body = (
      <Text muted small>
        Couldn't load the current contract terms. Reload the page to try again.
      </Text>
    );
  } else {
    body = (
      <div className="max-w-md space-y-4">
        <Skeleton className="h-9 w-full" />
        <Skeleton className="h-9 w-full" />
        <Skeleton className="h-9 w-full" />
      </div>
    );
  }

  return (
    <Page.Section>
      {/* Secondary section below TumUsageSection: suppress the area eyebrow. */}
      <Page.Section.Title area="">
        TUM Contract (PLATFORM ADMIN VIEW ONLY)
      </Page.Section.Title>
      <Page.Section.Description>
        Set this organization's contracted tokens under management terms.
        Customers never see this section or the alert email.
      </Page.Section.Description>
      <Page.Section.Body>{body}</Page.Section.Body>
    </Page.Section>
  );
};

function ContractForm({
  initial,
}: {
  // The loaded contract terms the fields seed from at mount.
  initial: TokensUnderManagement;
}): JSX.Element {
  const queryClient = useQueryClient();

  const [tokenLimit, setTokenLimit] = useState(
    () => initial.monthlyTokenLimit?.toString() ?? "",
  );
  const [alertEmail, setAlertEmail] = useState(() => initial.alertEmail ?? "");
  const [anchorDay, setAnchorDay] = useState(() =>
    initial.billingCycleAnchorDay.toString(),
  );

  const mutation = useSetBillingMetadataMutation({
    onSuccess: () => {
      void invalidateAllGetTokensUnderManagement(queryClient);
    },
  });

  const parsedLimit = tokenLimit.trim() === "" ? undefined : Number(tokenLimit);
  const parsedAnchorDay = Number(anchorDay);
  // The limit is a token count: the API schema only accepts integers.
  const limitInvalid =
    parsedLimit !== undefined &&
    (!Number.isInteger(parsedLimit) || parsedLimit < 0);
  const anchorDayInvalid =
    !Number.isInteger(parsedAnchorDay) ||
    parsedAnchorDay < 1 ||
    parsedAnchorDay > 31;

  const cycles = useMemo(() => cyclesFromTum(initial), [initial]);

  // The estimator prices whatever baseline the admin is currently proposing,
  // so a limit typed into the field above moves the contract value before
  // it's saved — that's the sizing workflow. A half-typed invalid entry falls
  // back to the saved terms instead of blanking the estimate out mid-keystroke.
  const estimatorBaseline = limitInvalid
    ? (initial.monthlyTokenLimit ?? null)
    : (parsedLimit ?? null);

  const handleSave = () => {
    mutation.mutate({
      request: {
        setBillingMetadataRequestBody: {
          monthlyTokenLimit: parsedLimit,
          alertEmail: alertEmail.trim() === "" ? undefined : alertEmail.trim(),
          billingCycleAnchorDay: parsedAnchorDay,
        },
      },
    });
  };

  return (
    <Stack gap={8}>
      <Stack gap={4} className="max-w-md">
        <Stack gap={2}>
          <Label htmlFor="tum-monthly-limit">
            Allowed TUM per month (tokens)
          </Label>
          <Input
            id="tum-monthly-limit"
            type="number"
            min={0}
            placeholder="Leave empty for no contracted limit"
            value={tokenLimit}
            onChange={setTokenLimit}
          />
        </Stack>
        <Stack gap={2}>
          <Label htmlFor="tum-alert-email">Alert email</Label>
          <Input
            id="tum-alert-email"
            type="email"
            placeholder="billing-alerts@customer.com"
            value={alertEmail}
            onChange={setAlertEmail}
          />
        </Stack>
        <Stack gap={2}>
          <Label htmlFor="tum-anchor-day">
            Billing cycle anchor day (1–31)
          </Label>
          <Input
            id="tum-anchor-day"
            type="number"
            min={1}
            max={31}
            value={anchorDay}
            onChange={setAnchorDay}
          />
        </Stack>
        <Stack direction="horizontal" align="center" gap={3}>
          <Button
            onClick={handleSave}
            disabled={mutation.isPending || limitInvalid || anchorDayInvalid}
          >
            {mutation.isPending ? "SAVING..." : "SAVE CONTRACT TERMS"}
          </Button>
          {mutation.isSuccess && !mutation.isPending && (
            <Text muted small>
              Saved.
            </Text>
          )}
          {mutation.isError && (
            <Text small className="text-destructive">
              Failed to save contract terms.
            </Text>
          )}
        </Stack>
      </Stack>

      <div className="border-border border-t pt-6">
        {/* Suspense covers the chunk being in flight; it does NOT catch a
            rejected dynamic import. Without this boundary a failed chunk
            fetch — most often a stale tab requesting a hash that a deploy
            has since replaced — would propagate to the Page.Body boundary
            and take the contract form down with it. The estimate is an
            optional aside, so its failure has to stay inside this box. */}
        <ErrorBoundary
          onError={(error) => handleError(toError(error), { silent: true })}
          fallbackRender={() => (
            // Announced, because this replaces a skeleton long after the page
            // has settled — with no live region a screen reader user is never
            // told the estimate failed, or that a reload is the way back.
            <Text muted small role="alert">
              The contract estimate couldn't load. Your contract terms above are
              unaffected —{" "}
              <button
                type="button"
                className="underline underline-offset-2"
                onClick={() => window.location.reload()}
              >
                reload the page
              </button>{" "}
              to try again.
            </Text>
          )}
        >
          <Suspense
            fallback={
              <div className="space-y-4">
                <Skeleton className="h-5 w-48" />
                <Skeleton className="h-9 w-full max-w-lg" />
                <div className="grid gap-4 md:grid-cols-2">
                  <Skeleton className="h-44 w-full" />
                  <Skeleton className="h-44 w-full" />
                </div>
              </div>
            }
          >
            <ContractPriceEstimator
              baselineTokens={estimatorBaseline}
              cycles={cycles}
            />
          </Suspense>
        </ErrorBoundary>
      </div>
    </Stack>
  );
}
