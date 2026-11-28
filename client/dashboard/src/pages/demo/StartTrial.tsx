import { useEffect, useRef, useState } from "react";
import { Navigate, useLocation, useNavigate } from "react-router";
import { useCreateTrialPaymentMethodCheckoutMutation } from "@gram/client/react-query/createTrialPaymentMethodCheckout.js";
import { QueryParamTheme } from "@gram/client/models/operations/createtrialpaymentmethodcheckout.js";
import type { TrialCardRejection } from "@gram/client/models/components/trialcardrejection.js";
import { PaygCheckoutSkeleton } from "@/components/billing/payg-checkout-skeleton";
import { TrialCheckoutMount } from "@/components/billing/trial-checkout-mount";
import type { PaygCheckoutSession } from "@/components/billing/use-start-payg-checkout";
import { RequireScope } from "@/components/require-scope";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Card } from "@/components/ui/Card";
import { Spinner } from "@/components/ui/Spinner";
import { SessionContext, useSession, useSessionData } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { logoutToLogin } from "@/lib/logout-to-login";
import { getHttpStatusCode } from "@/lib/route-errors";
import { AuthShell } from "@/pages/login/components/auth-shell";

const SUPPORT_EMAIL = "support@speakeasy.com";
const SALES_EMAIL = "sales@speakeasy.com";
/** How long the page keeps checking for the trial before it asks the user. */
const TRIAL_CONFIRMATION_WINDOW_MS = 60_000;
const TRIAL_CONFIRMATION_INTERVAL_MS = 2_000;
/** Tolerance between this browser's clock and the server's. */
const CLOCK_SKEW_MS = 60_000;

type CheckoutFailure = "unavailable" | "not-startable" | "forbidden" | "other";

function checkoutFailure(error: unknown): CheckoutFailure {
  const status = getHttpStatusCode(error);
  if (status === 503) return "unavailable";
  if (status === 409) return "not-startable";
  if (status === 403) return "forbidden";
  return "other";
}

function usableCheckoutSession(
  session: PaygCheckoutSession | undefined,
): session is PaygCheckoutSession {
  return (
    !!session &&
    /^cs_(test|live)_/.test(session.clientSecret) &&
    /^pk_(test|live)_/.test(session.publishableKey)
  );
}

/**
 * Full-page gate for an organization whose free trial starts once an
 * organization admin saves a card. The trial itself is started by the server
 * after Stripe confirms the card, so this page only ever waits for the
 * session to report it.
 */
export default function StartTrial(): JSX.Element | null {
  const { session } = useSessionData();
  const location = useLocation();
  const returning = new URLSearchParams(location.search).has("session_id");

  if (!session) return null;

  const trialStarted =
    session.whitelisted ||
    (!session.trialPendingPaymentMethod && session.trial !== null);
  // "/" is where login lands: the auth gate sends it on into the organization.
  if (trialStarted) return <Navigate to="/" replace />;
  if (!session.trialPendingPaymentMethod && !returning) {
    return <Navigate to="/" replace />;
  }

  return (
    <SessionContext.Provider value={session}>
      <StartTrialPage key={session.activeOrganizationId} />
    </SessionContext.Provider>
  );
}

function StartTrialPage(): JSX.Element {
  const client = useSdkClient();
  const telemetry = useTelemetry();
  const session = useSession();

  useEffect(() => {
    if (!session.activeOrganizationId) return;
    telemetry.capture("start_trial_page_viewed", {
      organization_id: session.activeOrganizationId,
      organization_slug: session.organization.slug,
    });
  }, [session.activeOrganizationId, session.organization.slug, telemetry]);

  return (
    <AuthShell
      page="Start your trial"
      singleColumn
      showTerms={false}
      sectionClassName="justify-start pt-[10vh] bg-background"
      headerAction={
        <button
          type="button"
          onClick={() => void logoutToLogin(client)}
          className="auth-mono text-[13px] leading-none transition-colors hover:text-black"
        >
          Log out
        </button>
      }
    >
      <Card>
        <RequireScope
          scope="org:admin"
          level="section"
          fallback={<MemberStartTrial />}
        >
          <AdminStartTrial />
        </RequireScope>
        <Card.Footer>
          <div className="flex w-full justify-end gap-2">
            <Button
              variant="secondary"
              size="md"
              icon="arrow-right"
              iconAfter
              href="/explore-demo"
            >
              Explore a Live Demo
            </Button>
          </div>
        </Card.Footer>
      </Card>
    </AuthShell>
  );
}

function StartTrialHeader({
  title,
  children,
}: {
  title: string;
  children: React.ReactNode;
}): JSX.Element {
  return (
    <Card.Header className="border-b pb-3">
      <span className="text-eyebrow">14-day free trial</span>
      <h1 className="text-display-md">{title}</h1>
      <div className="flex max-w-lg flex-col gap-1 pt-1">{children}</div>
    </Card.Header>
  );
}

function MemberStartTrial(): JSX.Element {
  const { organization } = useSession();
  return (
    <>
      <StartTrialHeader title="Ask an admin to start your trial.">
        <p className="text-body-md">
          Your 14-day free trial starts when an organization admin adds a card.
        </p>
        <p className="text-body-sm text-muted">
          Ask an admin of <strong>{organization.name}</strong> to sign in and
          add one. The card is not charged during the trial.
        </p>
      </StartTrialHeader>
      <Card.Content className="pt-2">
        <p className="text-body-sm text-muted">
          After they add it, refresh this page to get started.
        </p>
      </Card.Content>
    </>
  );
}

function AdminStartTrial(): JSX.Element {
  const telemetry = useTelemetry();
  const { refetch, trialCardRejection } = useSession();
  const location = useLocation();
  const navigate = useNavigate();
  const returnedFromStripe = new URLSearchParams(location.search).has(
    "session_id",
  );
  const [confirming, setConfirming] = useState(returnedFromStripe);
  // When this attempt's Checkout started. A rejection recorded after it is
  // about the card just added; an earlier one is about a previous card.
  const [attemptStartedAt, setAttemptStartedAt] = useState<number | null>(() =>
    returnedFromStripe ? Date.now() : null,
  );
  const [checksPaused, setChecksPaused] = useState(false);
  const [checkAttempt, setCheckAttempt] = useState(0);
  const [checkout, setCheckout] = useState<PaygCheckoutSession | null>(null);
  const [failure, setFailure] = useState<CheckoutFailure | null>(null);
  const started = useRef(false);
  const sessionRefetch = useRef(refetch);
  sessionRefetch.current = refetch;

  const mutation = useCreateTrialPaymentMethodCheckoutMutation();
  const { mutate } = mutation;

  const startCheckout = () => {
    setFailure(null);
    setCheckout(null);
    setAttemptStartedAt(Date.now());
    mutate(
      { request: { theme: QueryParamTheme.Light } },
      {
        onSuccess: (created) => {
          if (!usableCheckoutSession(created)) {
            telemetry.capture("start_trial_checkout_error", {
              error: "unusable checkout session",
            });
            setFailure("other");
            return;
          }
          setCheckout(created);
        },
        onError: (error: unknown) => {
          const kind = checkoutFailure(error);
          telemetry.capture("start_trial_checkout_error", { error: kind });
          setFailure(kind);
          // Another admin may have just started the trial; a fresh session
          // takes the page away if so.
          if (kind === "not-startable") void sessionRefetch.current();
        },
      },
    );
  };
  const startCheckoutRef = useRef(startCheckout);
  startCheckoutRef.current = startCheckout;

  useEffect(() => {
    if (confirming || started.current) return;
    started.current = true;
    startCheckoutRef.current();
  }, [confirming]);

  const rejected =
    confirming &&
    !!trialCardRejection &&
    attemptStartedAt !== null &&
    trialCardRejection.rejectedAt.getTime() > attemptStartedAt - CLOCK_SKEW_MS;
  const rejectionReason = rejected ? trialCardRejection?.reason : undefined;

  useEffect(() => {
    if (!rejectionReason) return;
    telemetry.capture("start_trial_card_rejected", {
      reason: rejectionReason,
    });
  }, [rejectionReason, telemetry]);

  // Poll the session for a bounded window; the trial appears once Stripe has
  // told the server the card was saved, unless the server rejects the card.
  useEffect(() => {
    if (!confirming || checksPaused || rejected) return;
    let refreshing = false;
    const refresh = async () => {
      if (refreshing) return;
      refreshing = true;
      try {
        await sessionRefetch.current();
      } catch {
        /* Keep waiting within this attempt. */
      } finally {
        refreshing = false;
      }
    };
    void refresh();
    const interval = window.setInterval(
      () => void refresh(),
      TRIAL_CONFIRMATION_INTERVAL_MS,
    );
    const deadline = window.setTimeout(
      () => setChecksPaused(true),
      TRIAL_CONFIRMATION_WINDOW_MS,
    );
    return () => {
      window.clearInterval(interval);
      window.clearTimeout(deadline);
    };
  }, [confirming, checksPaused, rejected, checkAttempt]);

  const checkAgain = () => {
    setChecksPaused(false);
    setCheckAttempt((attempt) => attempt + 1);
  };

  const addCardAgain = () => {
    const search = new URLSearchParams(location.search);
    search.delete("session_id");
    void navigate(
      { pathname: location.pathname, search: search.toString() },
      { replace: true },
    );
    // A fresh Checkout session: the previous one is already used up.
    started.current = false;
    setCheckout(null);
    setChecksPaused(false);
    setConfirming(false);
  };

  if (rejected && trialCardRejection) {
    return (
      <>
        <StartTrialHeader title="Your card couldn't be used.">
          <p className="text-body-md">Your trial has not started yet.</p>
        </StartTrialHeader>
        <Card.Content className="flex flex-col items-start gap-4 pt-2">
          <Alert variant="warning" title={null}>
            <CardRejectionReason rejection={trialCardRejection} />
          </Alert>
          <Button size="md" onClick={addCardAgain}>
            Add a different card
          </Button>
        </Card.Content>
      </>
    );
  }

  if (confirming) {
    return (
      <>
        <StartTrialHeader title="Starting your trial.">
          <p className="text-body-md">
            Your card is saved. We are setting up your organization now.
          </p>
        </StartTrialHeader>
        <Card.Content className="flex flex-col items-start gap-4 pt-2">
          {checksPaused ? (
            <>
              <Alert variant="info" title={null}>
                Your trial is taking longer than usual to start. Select{" "}
                <strong>Check again</strong>. If it still does not start, email{" "}
                <a className="underline" href={`mailto:${SUPPORT_EMAIL}`}>
                  {SUPPORT_EMAIL}
                </a>
                .
              </Alert>
              <div className="flex gap-2">
                <Button size="md" onClick={checkAgain}>
                  Check again
                </Button>
                <Button variant="secondary" size="md" onClick={addCardAgain}>
                  Add a card again
                </Button>
              </div>
            </>
          ) : (
            <div className="flex items-center gap-2">
              <Spinner />
              <p role="status" className="text-body-sm text-muted">
                This usually takes a few seconds. Keep this page open.
              </p>
            </div>
          )}
        </Card.Content>
      </>
    );
  }

  return (
    <>
      <StartTrialHeader title="Add a card to start your trial.">
        <p className="text-body-md">
          Your 14-day free trial starts as soon as you save a card.
        </p>
        <p className="text-body-sm text-muted">
          The card is not charged during the trial.
        </p>
      </StartTrialHeader>
      <Card.Content className="flex flex-col gap-4 pt-4">
        {trialCardRejection && !failure ? (
          <Alert
            variant="warning"
            title="The last card added couldn't be used."
          >
            <CardRejectionReason rejection={trialCardRejection} />
          </Alert>
        ) : null}
        {failure ? (
          <CheckoutFailureNotice failure={failure} onRetry={startCheckout} />
        ) : checkout ? (
          <TrialCheckoutMount
            session={checkout}
            onComplete={() => {
              setChecksPaused(false);
              setCheckAttempt((attempt) => attempt + 1);
              setConfirming(true);
            }}
          />
        ) : (
          <PaygCheckoutSkeleton />
        )}
      </Card.Content>
    </>
  );
}

function CheckoutFailureNotice({
  failure,
  onRetry,
}: {
  failure: CheckoutFailure;
  onRetry: () => void;
}): JSX.Element {
  switch (failure) {
    case "not-startable":
      return (
        <Alert variant="info" title={null}>
          This organization can't start a free trial. Email{" "}
          <a className="underline" href={`mailto:${SALES_EMAIL}`}>
            {SALES_EMAIL}
          </a>{" "}
          to talk about access.
        </Alert>
      );
    case "forbidden":
      return (
        <Alert variant="info" title={null}>
          Only an organization admin can add the card. Ask an admin to sign in
          and add one.
        </Alert>
      );
    case "unavailable":
      return (
        <div className="flex flex-col items-start gap-4">
          <Alert variant="warning" title={null}>
            Adding a card is unavailable right now. Select{" "}
            <strong>Try again</strong> in a few minutes, or email{" "}
            <a className="underline" href={`mailto:${SUPPORT_EMAIL}`}>
              {SUPPORT_EMAIL}
            </a>
            .
          </Alert>
          <Button size="md" onClick={onRetry}>
            Try again
          </Button>
        </div>
      );
    case "other":
      return (
        <div className="flex flex-col items-start gap-4">
          <Alert variant="error" title={null}>
            Couldn't load the card form. Select <strong>Try again</strong>.
          </Alert>
          <Button size="md" onClick={onRetry}>
            Try again
          </Button>
        </div>
      );
  }
}

function CardRejectionReason({
  rejection,
}: {
  rejection: TrialCardRejection;
}): JSX.Element {
  switch (rejection.reason) {
    case "prepaid_card":
      return (
        <>
          Prepaid cards can't be used for a free trial. Add a credit or debit
          card.
        </>
      );
    case "card_used":
      return (
        <>
          This card was already used to start a free trial. Add a different
          card, or email{" "}
          <a className="underline" href={`mailto:${SALES_EMAIL}`}>
            {SALES_EMAIL}
          </a>
          .
        </>
      );
    default:
      return (
        <>This card can't be used for a free trial. Add a different card.</>
      );
  }
}
