import { Page } from "@/components/page-layout";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { isAttributionDim } from "@/pages/costs/taxonomy";
import { telemetryQuery } from "@gram/client/funcs/telemetryQuery";
import { telemetryQueryRiskTokens } from "@gram/client/funcs/telemetryQueryRiskTokens";
import { Dimension } from "@gram/client/models/components/queryfilter.js";
import { type GroupBy } from "@gram/client/models/components/querypayload.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import {
  invalidateAllGetTokensUnderManagement,
  useGetTokensUnderManagement,
} from "@gram/client/react-query/getTokensUnderManagement.js";
import { useSetBillingMetadataMutation } from "@gram/client/react-query/setBillingMetadata.js";
import { unwrapAsync } from "@gram/client/types/fp";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Info } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { BillingCyclePicker } from "./billing-cycle-picker";
import { type BillingCycle, cycleKey, cyclesFromTum } from "./billing-cycles";
import { stackModeFor } from "./breakdown-options";
import { BreakdownPicker } from "./breakdown-picker";
import { TokenUsagePanel } from "./token-usage-panel";
import { TumDetailsTable } from "./tum-details-table";
import { TumUsageCard } from "./tum-usage-card";

// Org-wide token breakdown for one billing cycle: stacked daily tokens by a
// selectable dimension, by token type, or by risk involvement — one unified
// picker drives all three. Reads the same analytics aggregates as the costs
// explorer (telemetry.query), scoped to the cycle. No data-availability
// pruning of the dimension list: the availability probe
// (telemetry.listAttributeKeys) is project-scoped and this page is org-level,
// so it would filter against the wrong project — dimensions without data
// simply chart as "(unset)". The generated hooks key their cache on
// gramSession only, so the queries are driven directly with payload-encoding
// keys.
function TumTokenBreakdown({ cycle }: { cycle: BillingCycle }): JSX.Element {
  const client = useGramContext();
  // The picker's selection, plus the last-picked dimension so switching to
  // token type or risk and back doesn't lose the grouping.
  const [breakdown, setBreakdown] = useState<string>(Dimension.DivisionName);
  const [dimension, setDimension] = useState<Dimension>(Dimension.DivisionName);
  const stackBy = stackModeFor(breakdown);
  const from = cycle.start;
  const to = cycle.end;

  const { data, isFetching } = useQuery({
    queryKey: [
      "tum-breakdown",
      from.toISOString(),
      to.toISOString(),
      dimension,
    ],
    throwOnError: false,
    queryFn: () =>
      unwrapAsync(
        telemetryQuery(client, {
          queryPayload: {
            from,
            to,
            groupBy: dimension as GroupBy,
            sortBy: "total_tokens",
            topN: 100,
            // Daily buckets; the panel rolls up to weekly/monthly client-side.
            granularitySeconds: 86400,
          },
        }),
      ),
  });
  const { data: riskData } = useQuery({
    queryKey: ["tum-breakdown-risk", from.toISOString(), to.toISOString()],
    throwOnError: false,
    queryFn: () =>
      unwrapAsync(
        telemetryQueryRiskTokens(client, {
          queryRiskTokensPayload: { from, to },
        }),
      ),
  });

  // Attribution cuts hide the "" (not-applicable) group, same as the costs page.
  const series = useMemo(() => {
    const ts = data?.timeseries ?? [];
    return isAttributionDim(dimension)
      ? ts.filter((s) => s.groupValue !== "")
      : ts;
  }, [data, dimension]);

  const riskPoints = riskData?.points ?? null;

  const breakdownPicker = (
    <BreakdownPicker
      value={breakdown}
      showRisk={riskPoints != null}
      onChange={(value) => {
        setBreakdown(value);
        // Only actual dimensions feed the query's group_by; the special modes
        // (total / token type / risk) keep the last-picked dimension.
        if (stackModeFor(value) === "group") {
          setDimension(value as Dimension);
        }
      }}
    />
  );

  return (
    <TokenUsagePanel
      series={series}
      stackBy={stackBy}
      breakdownPicker={breakdownPicker}
      riskPoints={riskPoints}
      loading={isFetching && !data}
    />
  );
}

export const TumUsageSection = (): JSX.Element => {
  const { data: tum } = useGetTokensUnderManagement();

  // The selected billing cycle scopes the usage bar and the breakdown chart.
  // Derived (not synced) so the current cycle is the default once TUM loads.
  const cycles = useMemo(() => (tum ? cyclesFromTum(tum) : []), [tum]);
  const [selectedKey, setSelectedKey] = useState<string | null>(null);
  const selectedCycle =
    cycles.find((c) => cycleKey(c) === selectedKey) ??
    cycles.find((c) => c.current) ??
    cycles[0] ??
    null;

  return (
    <Page.Section>
      <Page.Section.Title>Billing</Page.Section.Title>
      <Page.Section.Description>
        The volume of agent traffic Gram has processed, stored, and run security
        analysis on each billing cycle, measured in tokens.
      </Page.Section.Description>
      <Page.Section.Body>
        {tum && selectedCycle ? (
          <Stack gap={3} className="mb-6">
            <Stack direction="horizontal" align="center" gap={1}>
              <Type variant="body" className="font-medium">
                Tokens Under Management
              </Type>
              <SimpleTooltip tooltip="Counts tokens from agent sessions Gram has stored chats or tool calls for during the selected billing cycle. Compared against your contracted monthly allowance.">
                <Info className="text-muted-foreground h-4 w-4" />
              </SimpleTooltip>
              <div className="ml-auto">
                <BillingCyclePicker
                  cycles={cycles}
                  selected={selectedCycle}
                  onSelect={(c) => setSelectedKey(cycleKey(c))}
                />
              </div>
            </Stack>
            <TumUsageCard
              tokens={selectedCycle.tokens}
              limit={tum.monthlyTokenLimit ?? null}
            />
            <div className="mt-8">
              <TumTokenBreakdown cycle={selectedCycle} />
            </div>
            <div className="mt-4">
              <TumDetailsTable cycle={selectedCycle} />
            </div>
          </Stack>
        ) : (
          <div className="space-y-4">
            <Skeleton className="h-4 w-1/3" />
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-40 w-full" />
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
