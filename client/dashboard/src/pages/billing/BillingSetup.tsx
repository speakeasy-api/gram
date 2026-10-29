import { useGetStripeCheckoutStatus } from "@gram/client/react-query/getStripeCheckoutStatus.js";
import { useBillingState } from "@/components/billing/use-billing-state";
import { BillingTabs } from "@/components/billing/billing-tabs";
import { PaygCheckoutSkeleton } from "@/components/billing/payg-checkout-skeleton";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { Navigate, useLocation, useNavigate } from "react-router";
import { useQueryClient } from "@tanstack/react-query";
import { PaygCheckoutMount } from "@/components/billing/payg-checkout-mount";
import { PaygPriceList } from "@/components/billing/payg-price-list";
import { PaygPlanSection } from "@/components/billing/payg-plan-section";
import { BillingEmailSection } from "@/components/billing/billing-email-section";
import { useStartPaygCheckout } from "@/components/billing/use-start-payg-checkout";
import { SettingsPage } from "@/components/page-templates";
import { Alert } from "@/components/ui/Alert";
import { Stack } from "@/components/ui/Stack";
import { Spinner } from "@/components/ui/Spinner";
import { Text } from "@/components/ui/Text";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { useSession } from "@/contexts/Auth";
import { isNotFoundError } from "@/lib/route-errors";

function BillingPage({
  children,
  title,
  description,
}: {
  children: ReactNode;
  title: string;
  description: string;
}) {
  return (
    <SettingsPage
      scope={["org:read", "org:admin"]}
      area="Organization"
      title={title}
      description={description}
    >
      <BillingTabs value="payment-method">{children}</BillingTabs>
    </SettingsPage>
  );
}

export function BillingPaymentMethod(): JSX.Element {
  const { activeOrganizationId, organization } = useSession();
  const location = useLocation();
  const billing = useBillingState();
  if (billing.status === "needs-setup")
    return <Navigate to={`/${organization.slug}/billing/setup`} replace />;
  if (billing.status === "unavailable")
    return <Navigate to={`/${organization.slug}/billing`} replace />;
  return (
    <BillingPage
      title="Payment method"
      description="Manage your payment method and billing details."
    >
      {location.state?.billingSetupOrganizationId === activeOrganizationId && (
        <Alert variant="success">
          <Text role="status">
            Payment method saved. Billing setup is complete.
          </Text>
        </Alert>
      )}
      <div className="grid gap-8 pb-6 lg:grid-cols-2 lg:items-start lg:gap-10">
        <PaygPriceList />
        <div className="w-full max-w-lg lg:justify-self-end">
          <PaygPlanSection />
        </div>
      </div>
      <BillingEmailSection />
    </BillingPage>
  );
}

export function BillingSetup(): JSX.Element {
  const { activeOrganizationId } = useSession();
  // An organization switch discards Stripe's iframe, mutation, and confirmation state.
  return (
    <RequireScope scope={["org:read", "org:admin"]} level="page">
      <BillingSetupInner key={activeOrganizationId} />
    </RequireScope>
  );
}

function BillingSetupInner() {
  const { activeOrganizationId, organization, whitelisted, refetch } =
    useSession();
  const location = useLocation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  // Redirect-based methods still return to /billing?session_id=... . Never
  // treat that URL (or Stripe's callback) as proof that setup has completed.
  const returnedSessionId = new URLSearchParams(location.search).get(
    "session_id",
  );
  const [confirming, setConfirming] = useState(returnedSessionId !== null);
  const billing = useBillingState({ surface: "setup", confirming });
  const {
    checkoutEligible: eligible,
    trialLifecycle,
    subscription,
    fresh,
    checkoutConfirmed: confirmed,
  } = billing;
  const returnedCheckout = useGetStripeCheckoutStatus(
    { sessionId: returnedSessionId ?? "" },
    undefined,
    {
      enabled: eligible && returnedSessionId !== null,
      throwOnError: false,
      retry: false,
      refetchOnMount: "always",
      refetchInterval: (query) => (query.state.error ? false : 2000),
    },
  );
  const returnedCheckoutFresh =
    returnedCheckout.isFetchedAfterMount && !returnedCheckout.isFetching;
  const returnedCheckoutComplete =
    returnedSessionId === null ||
    (returnedCheckoutFresh &&
      !returnedCheckout.error &&
      returnedCheckout.data?.status === "complete" &&
      returnedCheckout.data.billingReady === true);
  const checkout = useStartPaygCheckout(activeOrganizationId);
  const { isPending: checkoutPending, startCheckout } = checkout;
  const started = useRef(false);
  const sessionRefetch = useRef(refetch);
  sessionRefetch.current = refetch;
  const destination = `/${organization.slug}/billing/payment-method`;

  useEffect(() => {
    if (
      !eligible ||
      confirming ||
      !fresh ||
      checkoutPending ||
      billing.status !== "needs-setup" ||
      started.current
    )
      return;
    // A previous mount may still own the organization-wide lock. Only consume
    // this mount's start guard once that request has settled.
    started.current = true;
    startCheckout();
  }, [
    eligible,
    confirming,
    fresh,
    billing.status,
    checkoutPending,
    startCheckout,
  ]);

  useEffect(() => {
    if (
      !eligible ||
      returnedSessionId === null ||
      !returnedCheckoutFresh ||
      returnedCheckout.error
    )
      return;
    const status = returnedCheckout.data?.status;
    if (status !== "open" && status !== "expired") return;
    // A canceled authorization is not a completed checkout. Resume it (or let
    // the existing backend expiry recovery replace it) rather than polling forever.
    started.current = false;
    setConfirming(false);
    const search = new URLSearchParams(location.search);
    search.delete("session_id");
    void navigate(
      { pathname: location.pathname, search: search.toString() },
      { replace: true },
    );
  }, [
    eligible,
    returnedSessionId,
    returnedCheckoutFresh,
    returnedCheckout.error,
    returnedCheckout.data?.status,
    location.pathname,
    location.search,
    navigate,
  ]);

  useEffect(() => {
    if (!confirming || !eligible) return;
    let cancelled = false;
    const refresh = async () => {
      try {
        await sessionRefetch.current();
      } catch {
        /* Keep waiting; retry below. */
      }
    };
    void refresh();
    const timer = window.setInterval(() => {
      if (!cancelled) void refresh();
    }, 2000);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [confirming, eligible]);

  useEffect(() => {
    if (!confirming || !confirmed || !whitelisted || !returnedCheckoutComplete)
      return;
    void queryClient.invalidateQueries({ queryKey: ["@gram/client", "usage"] });
    void navigate(destination, {
      replace: true,
      state: {
        billingSetupComplete: true,
        billingSetupOrganizationId: activeOrganizationId,
      },
    });
  }, [
    confirming,
    confirmed,
    returnedCheckoutComplete,
    whitelisted,
    queryClient,
    navigate,
    destination,
    activeOrganizationId,
  ]);

  // Any existing subscription belongs in payment management, even if it needs
  // attention. In particular a card-backed trial must not create another one.
  if (
    eligible &&
    !confirming &&
    fresh &&
    billing.status === "manage" &&
    !subscription.error
  )
    return <Navigate to={destination} replace />;

  return (
    <BillingPage
      title={confirming ? "Confirming billing setup" : "Billing setup"}
      description={
        confirming
          ? "Your payment method is being confirmed."
          : trialLifecycle === "active"
            ? "Add a payment method to start pay as you go when your trial ends."
            : "Add a payment method to activate pay as you go."
      }
    >
      {!eligible ? (
        <Alert variant="info">
          <Text>
            Billing setup is not available. Contact your organization
            administrator or our team for help.
          </Text>
        </Alert>
      ) : confirming ? (
        <Stack gap={4} align="start">
          <Stack direction="horizontal" gap={2} align="center">
            <Spinner />
            <Text role="status" muted>
              {returnedCheckout.data?.status === "complete" &&
              returnedCheckout.data.billingReady === false
                ? "Your payment is still processing. Some payment methods take several days. You can safely leave this page; billing will update after confirmation."
                : "Waiting for payment confirmation. This can take a moment; you can safely leave this page."}
            </Text>
          </Stack>
          {returnedCheckout.error && (
            <Alert variant="error">
              <Text>
                Could not verify the returned checkout. Try again before
                continuing.
              </Text>
            </Alert>
          )}
          {subscription.error && !isNotFoundError(subscription.error) && (
            <Alert variant="error">
              <Text>Could not refresh billing. Retrying automatically.</Text>
            </Alert>
          )}
          <Button
            variant="secondary"
            onClick={() => {
              void subscription.refetch();
              if (returnedSessionId !== null) void returnedCheckout.refetch();
            }}
          >
            Check again
          </Button>
        </Stack>
      ) : subscription.error && !isNotFoundError(subscription.error) ? (
        <Stack gap={4} align="start">
          <Alert variant="error">
            <Text>
              Could not load your subscription. No checkout was started.
            </Text>
          </Alert>
          <Button onClick={() => void subscription.refetch()}>Retry</Button>
        </Stack>
      ) : checkout.error ? (
        <Stack gap={4} align="start">
          <Alert variant="error">
            <Text>{checkout.error}</Text>
          </Alert>
          <Button
            onClick={checkout.startCheckout}
            disabled={checkout.isPending}
          >
            Retry checkout
          </Button>
        </Stack>
      ) : checkout.session ? (
        <PaygCheckoutMount
          session={checkout.session}
          onComplete={() => {
            setConfirming(true);
            void subscription.refetch();
          }}
        />
      ) : (
        <PaygCheckoutSkeleton />
      )}
    </BillingPage>
  );
}
