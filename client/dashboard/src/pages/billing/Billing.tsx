import { FeatureRequestModal } from "@/components/FeatureRequestModal";
import { Page } from "@/components/page-layout";
import {
  ProductTier,
  ProductTierBadge,
  productTierColors,
} from "@/components/product-tier-badge";
import { Card, Cards, CardSkeleton } from "@/components/ui/card";
import { Heading } from "@/components/ui/heading";
import { Skeleton } from "@/components/ui/skeleton";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useSession } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";
import { TierLimits } from "@gram/client/models/components";
import {
  useGetCreditUsage,
  useGetPeriodUsage,
  useGetUsageTiers,
} from "@gram/client/react-query";
import { PolarEmbedCheckout } from "@polar-sh/checkout/embed";
import { cn, Stack } from "@speakeasy-api/moonshine";
import { Info } from "lucide-react";
import { useEffect, useState } from "react";

export default function Billing() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <UsageSection />
        <UsageTiers />
      </Page.Body>
    </Page>
  );
}

const UsageSection = () => {
  const session = useSession();
  const [isCreditUpgradeModalOpen, setIsCreditUpgradeModalOpen] =
    useState(false);

  const { data: creditUsage } = useGetCreditUsage();
  const { data: periodUsage } = useGetPeriodUsage(undefined, undefined, {
    throwOnError: !getServerURL().includes("localhost"),
  });

  const UsageItem = ({
    label,
    tooltip,
    value,
    included,
    overageIncrement,
    noMax,
  }: {
    label: string;
    tooltip: string;
    value: number;
    included: number;
    overageIncrement: number;
    noMax?: boolean;
  }) => {
    return (
      <Stack gap={3} className="mb-6">
        <Stack direction="horizontal" align="center" gap={1}>
          <Type variant="body" className="font-medium">
            {label}
          </Type>
          <SimpleTooltip tooltip={tooltip}>
            <Info className="w-4 h-4 text-muted-foreground" />
          </SimpleTooltip>
        </Stack>
        <UsageProgress
          value={value}
          included={included}
          overageIncrement={overageIncrement}
          noMax={noMax}
        />
      </Stack>
    );
  };

  return (
    <Page.Section>
      <Page.Section.Title>Usage</Page.Section.Title>
      <Page.Section.Description>
        A summary of your organization's usage this period. Please visit the
        billing portal to see complete details or manage your account.
      </Page.Section.Description>
      <Page.Section.Body>
        <div className="space-y-4">
          {periodUsage ? (
            <>
              <UsageItem
                label="Tool Calls"
                tooltip="The number of tool calls processed this period across all your organization's MCP servers."
                value={periodUsage.toolCalls}
                included={periodUsage.maxToolCalls || 1000}
                overageIncrement={periodUsage.maxToolCalls}
                noMax={session.gramAccountType === "enterprise"}
              />
              <UsageItem
                label="Servers"
                tooltip="The number of MCP servers enabled across your organization. Note that this shows the current number of enabled servers, but you will be billed on the maximum number active simultaneously during the billing period."
                value={periodUsage.actualEnabledServerCount}
                included={periodUsage.maxServers || 1}
                overageIncrement={1}
                noMax={session.gramAccountType === "enterprise"}
              />
            </>
          ) : (
            <>
              <Skeleton className="h-4 w-1/3" />
              <Skeleton className="h-4 w-full" />
              <Skeleton className="h-4 w-1/3" />
              <Skeleton className="h-4 w-full" />
            </>
          )}
          {creditUsage ? (
            <UsageItem
              label="Playground Credits"
              tooltip="The number of credits used this month for AI-powered dashboard experiences."
              value={creditUsage.creditsUsed}
              included={creditUsage.monthlyCredits}
              overageIncrement={creditUsage.monthlyCredits}
            />
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

const UsageTiers = () => {
  const { data: usageTiers, isLoading } = useGetUsageTiers();
  const session = useSession();
  const client = useSdkClient();
  const [checkoutLink, setCheckoutLink] = useState("");

  useEffect(() => {
    client.usage.createCheckout().then((link) => {
      setCheckoutLink(link);
      PolarEmbedCheckout.init(); // This must go here or else the checkout link won't open in an embedded window
    });
  }, []);

  // This must be initialized AFTER the link is set (more specifically, AFTER the PolarEmbedCheckout.init() call)
  const upgradeCTA = checkoutLink ? (
    <a
      href={checkoutLink}
      data-polar-checkout
      data-polar-checkout-theme="light"
      className="inline-flex"
    >
      <Page.Section.CTA>
        UPGRADE
      </Page.Section.CTA>
    </a>
  ) : null;

  const polarPortalCTA = (
    <Page.Section.CTA
      onClick={() => {
        client.usage.createCustomerSession().then((link) => {
          window.open(link, "_blank");
        });
      }}
      disabled={session.gramAccountType === "enterprise"}
    >
      MANAGE BILLING
    </Page.Section.CTA>
  );

  if (!usageTiers) {
    return <Cards isLoading={true} />;
  }

  const enterpriseTier = "Enterprise";

  const UsageCard = ({
    name,
    tier,
    active,
    previousTier,
  }: {
    name: string;
    tier: TierLimits;
    active: boolean;
    previousTier?: ProductTier;
  }) => {
    const price =
      name === enterpriseTier
        ? "Tailored pricing"
        : `$${tier.basePrice.toLocaleString()}`;

    const ringColor = productTierColors(name.toLowerCase() as ProductTier).ring;

    return (
      <Card className={cn("w-full p-6", active && `ring-2 ${ringColor}`)}>
        <Card.Header>
          <Card.Title>
            <Stack gap={1}>
              <ProductTierBadge tier={name.toLowerCase() as ProductTier} />
              <Heading variant="h2">{price}</Heading>
            </Stack>
          </Card.Title>
        </Card.Header>
        <Card.Content>
          <Stack gap={8}>
            <Stack gap={1}>
              <Type
                mono
                muted
                small
                variant="subheading"
                className="font-medium uppercase"
              >
                {previousTier
                  ? `Everything from ${previousTier}, plus`
                  : "Features"}
              </Type>
              <ul className="list-inside space-y-1">
                {tier.featureBullets.map((bullet) => (
                  <li>
                    <span className="text-muted-foreground/60">✓</span> {bullet}
                  </li>
                ))}
              </ul>
            </Stack>
            {tier.includedBullets && tier.includedBullets.length > 0 && (
              <Stack gap={1}>
                <Type
                  mono
                  muted
                  small
                  variant="subheading"
                  className="font-medium uppercase"
                >
                  Included
                </Type>
                <ul className="list-inside space-y-1">
                  {tier.includedBullets.map((bullet) => (
                    <li>
                      <span className="text-muted-foreground/60">✓</span>{" "}
                      {bullet}
                    </li>
                  ))}
                </ul>
              </Stack>
            )}
          </Stack>
        </Card.Content>
      </Card>
    );
  };

  return (
    <Page.Section>
      <Page.Section.Title>Pricing</Page.Section.Title>
      <Page.Section.Description>
        A breakdown of our pricing tiers.
      </Page.Section.Description>
      {session.gramAccountType === "free" ? upgradeCTA : polarPortalCTA}
      <Page.Section.Body>
        <Stack direction={"horizontal"} gap={4}>
          {isLoading ? (
            <>
              <CardSkeleton />
              <CardSkeleton />
              <CardSkeleton />
            </>
          ) : (
            <>
              <UsageCard
                name={"Free"}
                tier={usageTiers.free}
                active={session.gramAccountType === "free"}
              />
              <UsageCard
                name={"Pro"}
                tier={usageTiers.pro}
                previousTier="free"
                active={session.gramAccountType === "pro"}
              />
              <UsageCard
                name={enterpriseTier}
                tier={usageTiers.enterprise}
                previousTier="pro"
                active={session.gramAccountType === "enterprise"}
              />
            </>
          )}
        </Stack>
      </Page.Section.Body>
    </Page.Section>
  );
};

const UsageProgress = ({
  value,
  included,
  overageIncrement,
  noMax,
}: {
  value: number;
  included: number;
  overageIncrement: number;
  noMax?: boolean;
}) => {
  if (noMax) {
    included = Math.max(1, value * 1.5);
  }

  const anyOverage = value > included;
  const overageMax = anyOverage
    ? Math.ceil((value - included + 1) / overageIncrement) * overageIncrement // Compute next increment. +1 because we always want to show the next increment.
    : 0;
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
        className="h-full bg-success-default transition-all duration-300"
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
        className="h-full bg-warning-default transition-all duration-300"
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
        className="absolute top-6 text-xs text-muted-foreground whitespace-nowrap"
        style={{ right: `${101 - includedWidth}%` }}
      >
        {anyOverage
          ? `Included: ${included.toLocaleString()}`
          : `${value.toLocaleString()} / ${
              noMax ? "No limit" : included.toLocaleString()
            }`}
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
            className="absolute top-6 text-xs text-muted-foreground whitespace-nowrap"
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
