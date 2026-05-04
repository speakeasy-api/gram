import { Page } from "@/components/page-layout";
import { ProductTierBadge } from "@/components/product-tier-badge";
import { productTierColors } from "@/components/product-tier-utils";
import { Card, Cards, CardSkeleton } from "@/components/ui/card";
import { Heading } from "@/components/ui/heading";
import { Skeleton } from "@/components/ui/skeleton";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useIsAdmin } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { ProductTier, useProductTier } from "@/hooks/useProductTier";
import { getServerURL } from "@/lib/utils";
import { TierLimits } from "@gram/client/models/components";
import {
  useGetCreditUsage,
  useGetPeriodUsage,
  useGetUsageTiers,
} from "@gram/client/react-query";
import { PolarEmbedCheckout } from "@polar-sh/checkout/embed";
import { Button, cn, Stack } from "@speakeasy-api/moonshine";
import { Info } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { RequireScope } from "@/components/require-scope";
import { TopUpCTA, UsageProgress } from "@/components/billing/usage-controls";

export default function Billing() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope={["org:read", "org:admin"]} level="page">
          <BillingInner />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

export function BillingInner() {
  const productTier = useProductTier();

  return (
    <>
      <UsageSection />
      {/* The product tiers / self serve billing section is DEPRECATED, and thus only shown to users already on a paid, non-enterprise tier */}
      {(productTier === "base_PAID" || productTier === "__deprecated__pro") && (
        <UsageTiers />
      )}
    </>
  );
}

const UsageSection = () => {
  const productTier = useProductTier();

  const isAdmin = useIsAdmin();

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
            <Info className="text-muted-foreground h-4 w-4" />
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
      <RequireScope scope="org:admin" level="section">
        <TopUpCTA />
      </RequireScope>
      <Page.Section.Body>
        <div className="space-y-4">
          {periodUsage ? (
            <>
              <UsageItem
                label="Tool Calls"
                tooltip="The number of tool calls processed this period across all your organization's MCP servers."
                value={periodUsage.toolCalls}
                included={periodUsage.includedToolCalls || 1000}
                overageIncrement={periodUsage.includedToolCalls}
                noMax={productTier === "enterprise"}
              />
              <UsageItem
                label="Servers"
                tooltip="The number of MCP servers enabled across your organization. Note that this shows the current number of enabled servers, but you will be billed on the maximum number active simultaneously during the billing period."
                value={periodUsage.actualEnabledServerCount}
                included={periodUsage.includedServers || 1}
                overageIncrement={1}
                noMax={productTier === "enterprise"}
              />
              {isAdmin && (
                <UsageItem
                  label="Chat Based Credits (Polar) (ADMIN VIEW ONLY)"
                  tooltip="The number of credits used this month for chat based products and other AI-powered dashboard experiences."
                  value={periodUsage.credits}
                  included={periodUsage.includedCredits}
                  overageIncrement={periodUsage.includedCredits}
                  noMax={productTier === "enterprise"}
                />
              )}
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
              label="Chat Based Credits"
              tooltip="The number of credits used this month for chat based products and other AI-powered dashboard experiences."
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
    </Page.Section>
  );
};

const UsageTiers = () => {
  const productTier = useProductTier();
  const { data: usageTiers, isLoading } = useGetUsageTiers();
  const client = useSdkClient();
  const telemetry = useTelemetry();
  const [checkoutLink, setCheckoutLink] = useState("");
  const [checkoutError, setCheckoutError] = useState(false);
  const [isLoadingCheckout, setIsLoadingCheckout] = useState(true);

  useEffect(() => {
    const fetchCheckoutLink = async () => {
      try {
        const link = await client.usage.createCheckout();
        if (!link) {
          console.error("Failed to create checkout link: received empty link");
          telemetry.capture("checkout_link_error", {
            error: "Received empty checkout link",
            accountType: productTier,
          });
          setCheckoutError(true);
          return;
        }
        setCheckoutLink(link);
        PolarEmbedCheckout.init();
      } catch (error) {
        console.error("Error creating checkout link:", error);
        telemetry.capture("checkout_link_error", {
          error:
            error instanceof Error
              ? error.message
              : "Failed to create checkout link",
          accountType: productTier,
        });
        setCheckoutError(true);
      } finally {
        setIsLoadingCheckout(false);
      }
    };

    fetchCheckoutLink();
  }, [client, telemetry, productTier]);

  const handleFallbackClick = useCallback(() => {
    telemetry.capture("checkout_fallback_clicked", {
      accountType: productTier,
    });
  }, [telemetry, productTier]);

  const UpgradeCTA = useMemo(() => {
    if (checkoutError) {
      return (
        <Page.Section.CTA>
          <div className="isolate">
            <Button asChild variant="primary">
              <a
                href="mailto:gram@speakeasyapi.dev?subject=Upgrade%20Account"
                className="inline-flex"
                onClick={handleFallbackClick}
              >
                ADD CARD
              </a>
            </Button>
          </div>
        </Page.Section.CTA>
      );
    }

    return (
      <Page.Section.CTA>
        {/* Isolate is needed to get the rainbow working */}
        <div className="isolate">
          <Button disabled={isLoadingCheckout} asChild variant="primary">
            <a
              href={checkoutLink}
              data-polar-checkout
              data-polar-checkout-theme={"light"}
              className="inline-flex"
            >
              ADD CARD
            </a>
          </Button>
        </div>
      </Page.Section.CTA>
    );
  }, [checkoutLink, checkoutError, isLoadingCheckout, handleFallbackClick]);

  const polarPortalCTA = (
    <Page.Section.CTA>
      <Button
        onClick={async () => {
          try {
            const link = await client.usage.createCustomerSession();
            if (!link) {
              console.error(
                "Failed to create customer session: received empty link",
              );
              telemetry.capture("customer_session_error", {
                error: "Received empty customer session link",
                accountType: productTier,
              });
              return;
            }
            window.open(link, "_blank");
          } catch (error) {
            console.error("Error creating customer session:", error);
            telemetry.capture("customer_session_error", {
              error:
                error instanceof Error
                  ? error.message
                  : "Failed to create customer session",
              accountType: productTier,
            });
          }
        }}
        disabled={productTier === "enterprise"}
      >
        MANAGE BILLING
      </Button>
    </Page.Section.CTA>
  );

  if (!usageTiers) {
    return <Cards isLoading={true} />;
  }

  const UsageCard = ({
    tier,
    tierLimits,
    active,
    previousTier,
  }: {
    tier: ProductTier;
    tierLimits: TierLimits;
    active: boolean;
    previousTier?: ProductTier;
  }) => {
    const price =
      tier === "enterprise"
        ? "Tailored pricing"
        : `$${tierLimits.basePrice.toLocaleString()}`;

    const ringColor = productTierColors(tier).ring;

    return (
      <Card className={cn("w-full p-6", active && `ring-2 ${ringColor}`)}>
        <Card.Header>
          <Card.Title>
            <Stack gap={1}>
              <ProductTierBadge tierOverride={tier} />
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
                {tierLimits.featureBullets.map((bullet) => (
                  <li>
                    <span className="text-muted-foreground/60">✓</span> {bullet}
                  </li>
                ))}
              </ul>
            </Stack>
            {tierLimits.includedBullets &&
              tierLimits.includedBullets.length > 0 && (
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
                    {tierLimits.includedBullets.map((bullet) => (
                      <li>
                        <span className="text-muted-foreground/60">✓</span>{" "}
                        {bullet}
                      </li>
                    ))}
                  </ul>
                </Stack>
              )}
            {tierLimits.addOnBullets && tierLimits.addOnBullets.length > 0 && (
              <Stack gap={1}>
                <Type
                  mono
                  muted
                  small
                  variant="subheading"
                  className="font-medium uppercase"
                >
                  Extras
                </Type>
                <ul className="list-inside space-y-1">
                  {tierLimits.addOnBullets.map((bullet) => (
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
      <RequireScope scope="org:admin" level="section">
        {productTier === "base" ? UpgradeCTA : polarPortalCTA}
      </RequireScope>
      <Page.Section.Body>
        <Stack direction={"horizontal"} gap={4}>
          {isLoading ? (
            <>
              <CardSkeleton />
              <CardSkeleton />
            </>
          ) : (
            <>
              {/* Show the paid base tier card only if the user is on the tier (otherwise it's deprecated) */}
              {productTier === "base_PAID" && (
                <UsageCard
                  tier="base_PAID"
                  tierLimits={usageTiers.free}
                  active={productTier === "base_PAID"}
                />
              )}
              {/* Keep this so we can show it to users who are still on the old pricing tier */}
              {productTier === "__deprecated__pro" && (
                <UsageCard
                  tier="__deprecated__pro"
                  tierLimits={usageTiers.pro}
                  previousTier="base"
                  active={productTier === "__deprecated__pro"}
                />
              )}
              <UsageCard
                tier="enterprise"
                tierLimits={usageTiers.enterprise}
                previousTier="base"
                active={productTier === "enterprise"}
              />
            </>
          )}
        </Stack>
      </Page.Section.Body>
    </Page.Section>
  );
};
