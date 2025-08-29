import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import { Page } from "@/components/page-layout";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useIsAdmin, useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import { useGetCreditUsage, useGetPeriodUsage } from "@gram/client/react-query";
import { PolarEmbedCheckout } from "@polar-sh/checkout/embed";
import { Stack, cn } from "@speakeasy-api/moonshine";
import { Info } from "lucide-react";
import { useEffect, useState } from "react";

export default function Billing() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <BillingPage />
      </Page.Body>
    </Page>
  );
}

const BillingPage = () => {
  const session = useSession();
  const [isCreditUpgradeModalOpen, setIsCreditUpgradeModalOpen] =
    useState(false);

  const { data: creditUsage } = useGetCreditUsage();
  const { data: periodUsage } = useGetPeriodUsage(undefined, undefined, {
    throwOnError: !getServerURL().includes("localhost"),
  });

  const isAdmin = useIsAdmin();

  return (
    <Page.Section>
      <Page.Section.Title>Usage</Page.Section.Title>
      <Page.Section.Description>
        A summary of your organization's usage this period. Please visit the
        billing portal to see complete details or manage your account.
      </Page.Section.Description>
      <Page.Section.CTA>
        {session.gramAccountType === "free" ? (
          <UpgradeLink />
        ) : (
          <PolarPortalLink />
        )}
      </Page.Section.CTA>
      <Page.Section.Body>
        <div className="space-y-4">
          {/* TODO: DO NOT SHIP THIS UNTIL THE PERIOD USAGE REFLECTS THE ORG (SDK BUG SOLVED) */}
          {isAdmin &&
            (periodUsage ? (
              <>
                <div>
                  <Stack direction="horizontal" align="center" gap={1}>
                    <Type variant="body" className="font-medium">
                      Tool Calls
                    </Type>
                    <SimpleTooltip tooltip="The number of tool calls processed this period across all your organization's MCP servers.">
                      <Info className="w-4 h-4 text-muted-foreground" />
                    </SimpleTooltip>
                  </Stack>
                  <UsageProgress
                    value={periodUsage.toolCalls}
                    included={periodUsage.maxToolCalls}
                    overageIncrement={periodUsage.maxToolCalls}
                  />
                </div>
                <div>
                  <Stack direction="horizontal" align="center" gap={1}>
                    <Type variant="body" className="font-medium">
                      Servers
                    </Type>
                    <SimpleTooltip tooltip="The number of public MCP servers across your organization.">
                      <Info className="w-4 h-4 text-muted-foreground" />
                    </SimpleTooltip>
                  </Stack>
                  <UsageProgress
                    value={periodUsage.actualPublicServerCount} // TODO: We are using this because the value coming from Polar is not correctly scoped to the organization because of a bug in the SDK
                    included={periodUsage.maxServers}
                    overageIncrement={1}
                  />
                </div>
              </>
            ) : (
              <>
                <Skeleton className="h-4 w-1/3" />
                <Skeleton className="h-4 w-full" />
                <Skeleton className="h-4 w-1/3" />
                <Skeleton className="h-4 w-full" />
              </>
            ))}
          {creditUsage ? (
            <div>
              <Stack direction="horizontal" align="center" gap={1}>
                <Type variant="body" className="font-medium">
                  Playground Credits
                </Type>
                <SimpleTooltip tooltip="The number of credits used this month for AI-powered dashboard experiences.">
                  <Info className="w-4 h-4 text-muted-foreground" />
                </SimpleTooltip>
              </Stack>
              <UsageProgress
                value={creditUsage.creditsUsed}
                included={creditUsage.monthlyCredits}
                overageIncrement={creditUsage.monthlyCredits}
              />
            </div>
          ) : (
            <>
              <Skeleton className="h-4 w-1/3" />
              <Skeleton className="h-4 w-full" />
            </>
          )}
        </div>
      </Page.Section.Body>
      <FeatureRequestModal
        isOpen={isCreditUpgradeModalOpen}
        onClose={() => setIsCreditUpgradeModalOpen(false)}
        title="Increase Credit Limit"
        description={`To increase your monthly credit limit upgrade from the ${session.gramAccountType} account type. Someone should be in touch shortly, or feel free to book a meeting directly to upgrade.`}
        actionType="credit_upgrade"
        accountUpgrade
      />
    </Page.Section>
  );
};

const UsageProgress = ({
  value,
  included,
  overageIncrement,
}: {
  value: number;
  included: number;
  overageIncrement: number;
}) => {
  const anyOverage = value > included;
  const overageMax =
    Math.ceil((value - included + 1) / overageIncrement) * overageIncrement; // Compute next increment. +1 because we always want to show the next increment.
  const totalMax = included + overageMax;

  // Calculate the proportional width for the included section
  const includedWidth = (included / totalMax) * 100;
  const overageWidth = (overageMax / totalMax) * 100;

  const includedProgress = (
    <div
      className={cn(
        "h-4 bg-muted dark:bg-neutral-800 rounded-md overflow-hidden relative",
        anyOverage && "rounded-r-none"
      )}
      style={{ width: `${includedWidth}%` }}
    >
      <div
        className="h-full bg-gradient-to-r from-green-400 to-green-600 dark:from-green-700 dark:to-green-500 transition-all duration-300"
        style={{
          width: `${Math.min((value / included) * 100, 100)}%`,
        }}
      />
    </div>
  );

  const overageProgress = anyOverage ? (
    <div
      className="h-4 bg-muted dark:bg-neutral-800 rounded-r-md overflow-hidden relative"
      style={{ width: `${overageWidth}%` }}
    >
      <div
        className="h-full bg-gradient-to-r from-yellow-400 to-yellow-600 dark:from-yellow-700 dark:to-yellow-500 transition-all duration-300"
        style={{
          width: `${Math.min(((value - included) / overageMax) * 100, 100)}%`,
        }}
      />
    </div>
  ) : null;

  return (
    <div className="relative">
      {/* Progress bars */}
      <div className="flex w-full">
        {includedProgress}
        {overageProgress}
      </div>
      {/* Included label underneath, always show */}
      <div
        className="absolute top-5 text-xs text-muted-foreground whitespace-nowrap"
        style={{ right: `${101 - includedWidth}%` }}
      >
        {anyOverage
          ? `Included: ${included.toLocaleString()}`
          : `${value.toLocaleString()} / ${included.toLocaleString()}`}
      </div>

      {/* Divider line and labels for overage */}
      {anyOverage && (
        <>
          <div
            className="absolute top-0 w-[2px] h-8 bg-neutral-600"
            style={{ left: `${includedWidth}%` }}
          />
          {/* Overage label underneath */}
          <div
            className="absolute top-5 text-xs text-muted-foreground whitespace-nowrap"
            style={{ left: `${includedWidth + 1}%` }}
          >
            Extra: {(value - included).toLocaleString()}
          </div>

          {/* Additional overage increment dividers */}
          {Array.from(
            { length: Math.floor((value - included) / overageIncrement) },
            (_, index) => {
              const incrementPosition =
                includedWidth +
                (((index + 1) * overageIncrement) / totalMax) * 100;
              return (
                <div
                  key={index}
                  className="absolute top-0 w-[2px] h-5 bg-neutral-600"
                  style={{ left: `${incrementPosition}%` }}
                />
              );
            }
          )}
        </>
      )}
    </div>
  );
};

const PolarPortalLink = () => {
  const client = useSdkClient();
  const session = useSession();

  return (
    <Button
      onClick={() => {
        client.usage.createCustomerSession().then((link) => {
          window.open(link, "_blank");
        });
      }}
      disabled={session.gramAccountType === "enterprise"}
      tooltip={
        session.gramAccountType === "enterprise"
          ? "Enterprise: Contact support to manage billing"
          : undefined
      }
    >
      Manage Billing
    </Button>
  );
};

const UpgradeLink = () => {
  const [checkoutLink, setCheckoutLink] = useState("");
  const client = useSdkClient();

  useEffect(() => {
    PolarEmbedCheckout.init();
    client.usage.createCheckout().then((link) => {
      setCheckoutLink(link);
    });
  }, []);

  return (
    <a
      href={checkoutLink}
      data-polar-checkout
      data-polar-checkout-theme="light"
    >
      Upgrade
    </a>
  );
};
