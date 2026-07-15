import { Page } from "@/components/page-layout";
import { SettingsLayout } from "@/components/layouts/settings-layout";
import { ProductTierBadge } from "@/components/product-tier-badge";
import { productTierColors } from "@/components/product-tier-utils";
import { Card, Cards, CardSkeleton } from "@/components/ui/card";
import { Heading } from "@/components/ui/heading";
import { Skeleton } from "@/components/ui/skeleton";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { useIsPlatformAdmin } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { ProductTier, useProductTier } from "@/hooks/useProductTier";
import { getServerURL } from "@/lib/utils";
import { TierLimits } from "@gram/client/models/components/tierlimits.js";
import { useGetCreditUsage } from "@gram/client/react-query/getCreditUsage.js";
import { useGetPeriodUsage } from "@gram/client/react-query/getPeriodUsage.js";
import { useGetUsageTiers } from "@gram/client/react-query/getUsageTiers.js";
import { PolarEmbedCheckout } from "@polar-sh/checkout/embed";
import { Stack } from "@/components/ui/stack";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { Info } from "lucide-react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { RequireScope } from "@/components/require-scope";
import { TopUpCTA, UsageProgress } from "@/components/billing/usage-controls";
import {
  TumAdminSection,
  TumUsageSection,
} from "@/components/billing/tum-section";

export default function Billing(): JSX.Element {
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

function BillingInner() {
  const productTier = useProductTier();
  const isAdmin = useIsPlatformAdmin();

  return (
    <SettingsLayout>
      <SettingsLayout.Header
        title="Billing"
        subtitle="Usage, pricing, and account management for your organization."
      />
      <SettingsLayout.Body>
        {/* Enterprise contracts bill on tokens under management, so enterprise
        orgs see the TUM view instead of the self-serve usage meters. */}
        {productTier === "enterprise" ? (
          <>
            <TumUsageSection />
            {isAdmin && <TumAdminSection />}
          </>
        ) : (
          <>
            <UsageSection />
            {/* The product tiers / self serve billing section is DEPRECATED, and thus only shown to users already on a paid, non-enterprise tier */}
            {(productTier === "base_PAID" ||
              productTier === "__deprecated__pro") && <UsageTiers />}
          </>
        )}
      </SettingsLayout.Body>
    </SettingsLayout>
  );
}

const UsageSection = () => {
  const productTier = useProductTier();

  const isAdmin = useIsPlatformAdmin();

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
    <SettingsLayout.Group
      label="Usage"
      description="A summary of your organization's usage this period. Please visit the billing portal to see complete details or manage your account."
      actions={
        <RequireScope scope="org:admin" level="section">
          <TopUpCTA />
        </RequireScope>
      }
    >
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
    </SettingsLayout.Group>
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

    void fetchCheckoutLink();
  }, [client, telemetry, productTier]);

  const handleFallbackClick = useCallback(() => {
    telemetry.capture("checkout_fallback_clicked", {
      accountType: productTier,
    });
  }, [telemetry, productTier]);

  const upgradeCTA = useMemo(() => {
    if (checkoutError) {
      return (
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
      );
    }

    return (
      // Isolate is needed to get the rainbow working
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
    );
  }, [checkoutLink, checkoutError, isLoadingCheckout, handleFallbackClick]);

  const polarPortalCTA = (
    <Button
      onClick={() => {
        void (async () => {
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
        })();
      }}
      disabled={productTier === "enterprise"}
    >
      MANAGE BILLING
    </Button>
  );

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
                  <li key={bullet}>
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
                      <li key={bullet}>
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
                    <li key={bullet}>
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
    <SettingsLayout.Group
      label="Pricing"
      description="A breakdown of our pricing tiers."
      actions={
        <RequireScope scope="org:admin" level="section">
          {productTier === "base" ? upgradeCTA : polarPortalCTA}
        </RequireScope>
      }
    >
      {!usageTiers ? (
        <Cards isLoading={true} />
      ) : (
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
      )}
    </SettingsLayout.Group>
  );
};
