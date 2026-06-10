import { Page } from "@/components/page-layout";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { TokensUnderManagement } from "@gram/client/models/components";
import {
  invalidateAllGetTokensUnderManagement,
  useGetTokensUnderManagement,
  useSetBillingMetadataMutation,
} from "@gram/client/react-query";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Info } from "lucide-react";
import { useEffect, useState } from "react";
import { UsageProgress } from "./usage-controls";

const cycleDateFormat = new Intl.DateTimeFormat("en-US", {
  month: "short",
  day: "numeric",
  year: "numeric",
  timeZone: "UTC",
});

function formatCycleRange(tum: TokensUnderManagement): string {
  return `${cycleDateFormat.format(tum.periodStart)} – ${cycleDateFormat.format(tum.periodEnd)}`;
}

export const TumUsageSection = (): JSX.Element => {
  const { data: tum } = useGetTokensUnderManagement();

  return (
    <Page.Section>
      <Page.Section.Title>Tokens Under Management</Page.Section.Title>
      <Page.Section.Description>
        The volume of agent traffic Gram has processed, stored, and run security
        analysis on this billing cycle, measured in tokens.
      </Page.Section.Description>
      <Page.Section.Body>
        {tum ? (
          <Stack gap={3} className="mb-6">
            <Stack direction="horizontal" align="center" gap={1}>
              <Type variant="body" className="font-medium">
                Tokens Under Management
              </Type>
              <SimpleTooltip tooltip="Counts tokens from agent sessions Gram has stored chats or tool calls for. Compared against your contracted monthly allowance.">
                <Info className="text-muted-foreground h-4 w-4" />
              </SimpleTooltip>
              <Type muted small className="ml-auto">
                Billing cycle: {formatCycleRange(tum)}
              </Type>
            </Stack>
            <UsageProgress
              value={tum.tokens}
              included={tum.monthlyTokenLimit ?? 0}
              overageIncrement={tum.monthlyTokenLimit ?? 1}
              noMax={tum.monthlyTokenLimit == null}
            />
          </Stack>
        ) : (
          <div className="space-y-4">
            <Skeleton className="h-4 w-1/3" />
            <Skeleton className="h-4 w-full" />
          </div>
        )}
      </Page.Section.Body>
    </Page.Section>
  );
};

export const TumAdminSection = (): JSX.Element => {
  const queryClient = useQueryClient();
  const { data: tum } = useGetTokensUnderManagement();

  const [tokenLimit, setTokenLimit] = useState("");
  const [alertEmail, setAlertEmail] = useState("");
  const [anchorDay, setAnchorDay] = useState("1");

  // Prefill the form once the current contract terms load.
  useEffect(() => {
    if (!tum) return;
    setTokenLimit(tum.monthlyTokenLimit?.toString() ?? "");
    setAlertEmail(tum.alertEmail ?? "");
    setAnchorDay(tum.billingCycleAnchorDay.toString());
  }, [tum]);

  const mutation = useSetBillingMetadataMutation({
    onSuccess: () => {
      void invalidateAllGetTokensUnderManagement(queryClient);
    },
  });

  const parsedLimit = tokenLimit.trim() === "" ? undefined : Number(tokenLimit);
  const parsedAnchorDay = Number(anchorDay);
  const limitInvalid =
    parsedLimit !== undefined &&
    (!Number.isFinite(parsedLimit) || parsedLimit < 0);
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
      <Page.Section.Title>TUM Contract (ADMIN VIEW ONLY)</Page.Section.Title>
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
              disabled={mutation.isPending || limitInvalid || anchorDayInvalid}
            >
              {mutation.isPending ? "SAVING..." : "SAVE CONTRACT TERMS"}
            </Button>
            {mutation.isSuccess && !mutation.isPending && (
              <Type muted small>
                Saved.
              </Type>
            )}
            {mutation.isError && (
              <Type small className="text-destructive">
                Failed to save contract terms.
              </Type>
            )}
          </Stack>
        </Stack>
      </Page.Section.Body>
    </Page.Section>
  );
};
