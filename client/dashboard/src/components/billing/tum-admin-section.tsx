import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import {
  invalidateAllGetTokensUnderManagement,
  useGetTokensUnderManagement,
} from "@gram/client/react-query/getTokensUnderManagement.js";
import { useSetBillingMetadataMutation } from "@gram/client/react-query/setBillingMetadata.js";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";

// Platform-admin form for the org's contracted tokens-under-management
// terms: monthly allowance, alert email, and the billing cycle anchor day.
export const TumAdminSection = (): JSX.Element => {
  const queryClient = useQueryClient();
  const { data: tum } = useGetTokensUnderManagement();

  const [tokenLimit, setTokenLimit] = useState("");
  const [alertEmail, setAlertEmail] = useState("");
  const [anchorDay, setAnchorDay] = useState("1");
  const [prefilled, setPrefilled] = useState(false);

  // Prefill the form once the current contract terms load. Once only — a
  // background refetch must not clobber in-progress edits.
  useEffect(() => {
    if (!tum || prefilled) return;
    setTokenLimit(tum.monthlyTokenLimit?.toString() ?? "");
    setAlertEmail(tum.alertEmail ?? "");
    setAnchorDay(tum.billingCycleAnchorDay.toString());
    setPrefilled(true);
  }, [tum, prefilled]);

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
    <Page.Section>
      <Page.Section.Title>
        TUM Contract (PLATFORM ADMIN VIEW ONLY)
      </Page.Section.Title>
      <Page.Section.Description>
        Set this organization's contracted tokens under management terms.
        Customers never see this section or the alert email.
      </Page.Section.Description>
      <Page.Section.Body>
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
              // Gated on the prefill: saving before the current terms load
              // would overwrite them with empty defaults.
              disabled={
                !prefilled ||
                mutation.isPending ||
                limitInvalid ||
                anchorDayInvalid
              }
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
      </Page.Section.Body>
    </Page.Section>
  );
};
