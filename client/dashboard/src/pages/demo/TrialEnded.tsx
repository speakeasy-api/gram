import { useEffect, useState } from "react";
import { useStartPaygCheckout } from "@/components/billing/use-start-payg-checkout";
import { ErrorAlert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Icon } from "@/components/ui/Icon";
import { RadioCard, RadioCardGroup } from "@/components/ui/RadioCard";
import { useSessionData } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { useTrialNow } from "@/hooks/useTrialNow";
import { getTrialLifecycleFromDates } from "@/lib/trial-status";
import { AuthShell } from "@/pages/login/components/auth-shell";
import { BookingCalendarModal } from "./components/booking-calendar/BookingCalendarModal";
import { useBookingCalendarModal } from "./components/booking-calendar/useBookingCalendarModal";
import { getGateCopy } from "./upgrade-gate-copy";
import { Card } from "@/components/ui/Card";
import { Navigate } from "react-router";

const SALES_EMAIL = "sales@speakeasy.com";

type TrialEndedChoice = "payg" | "sales";

export default function TrialEnded(): JSX.Element {
  const { session } = useSessionData();
  const trialNow = useTrialNow(session?.trial);

  if (getTrialLifecycleFromDates(session?.trial, trialNow) !== "expired") {
    return <Navigate to="/" replace />;
  }

  return <ExpiredTrialEnded />;
}

function ExpiredTrialEnded(): JSX.Element {
  const client = useSdkClient();
  const telemetry = useTelemetry();
  const { session } = useSessionData();
  const { openBookingCalendar, modalProps } = useBookingCalendarModal();
  const paygCheckout = useStartPaygCheckout(
    session?.activeOrganizationId ?? "",
  );
  const [choice, setChoice] = useState<TrialEndedChoice | null>(null);
  const now = new Date();
  const lifecycle = getTrialLifecycleFromDates(session?.trial, now);
  const gateCopy = getGateCopy(session?.trial, now);
  const copy =
    lifecycle === "expired"
      ? gateCopy
      : {
          dotClassName: "bg-(--vermilion)",
          status: "Trial ended",
          detail:
            "Your MCP servers, observability data, and policies are still here when you upgrade.",
        };

  useEffect(() => {
    if (!session?.user.email || !session.activeOrganizationId) return;

    telemetry.capture("trial_ended_page_viewed", {
      email: session.user.email,
      organization_id: session.activeOrganizationId,
      organization_name: session.organization?.name ?? "",
      organization_slug: session.organization?.slug ?? "",
    });
  }, [
    session?.activeOrganizationId,
    session?.organization?.name,
    session?.organization?.slug,
    session?.user.email,
    telemetry,
  ]);

  useEffect(() => {
    if (paygCheckout.error) setChoice(null);
  }, [paygCheckout.error]);

  const handleLogout = async () => {
    await client.auth.logout();
    window.location.href = "/login";
  };

  const handleChoiceChange = (value: string) => {
    const nextChoice = value as TrialEndedChoice;
    telemetry.capture("trial_ended_choice_selected", {
      choice: nextChoice,
      previous_choice: choice,
    });
    setChoice(nextChoice);
  };

  const openSalesCalendar = () => {
    openBookingCalendar({
      eventLabel: "Upgrade Trial — 30 min",
      formDefaults: { source: gateCopy.source, notes: gateCopy.notes },
      footer: (
        <Button variant="secondary" size="md" href={`mailto:${SALES_EMAIL}`}>
          Email {SALES_EMAIL}
        </Button>
      ),
      onClose: () => setChoice(null),
    });
  };

  return (
    <AuthShell
      page="Trial ended"
      singleColumn
      showTerms={false}
      sectionClassName="justify-start pt-[10vh] bg-background"
      headerAction={
        <button
          type="button"
          onClick={() => void handleLogout()}
          className="auth-mono text-[13px] leading-none transition-colors hover:text-black"
        >
          Log out
        </button>
      }
    >
      <Card>
        <Card.Header className="border-b pb-3">
          <span className="text-eyebrow">{copy.status}</span>
          <h1 className="text-display-md">Choose how to continue.</h1>
        </Card.Header>
        <Card.Content className="pt-2">
          <div className="flex flex-col gap-1 max-w-lg">
            <p className="text-body-md">
              Your trial has ended, but your workspace is still here. Choose an
              option that works best for your organization.
            </p>
            <p className="text-body-sm text-muted">{copy.detail}</p>
          </div>
          <div className="pt-3">
            <RadioCardGroup
              aria-label="Choose how to continue"
              orientation="horizontal"
              value={choice}
              onValueChange={handleChoiceChange}
              showIndicator={false}
            >
              <RadioCard
                value="payg"
                title={
                  <div className="flex gap-3">
                    <Icon
                      name="credit-card"
                      className="mt-0.5 ml-0.5 size-5 shrink-0"
                      aria-hidden="true"
                    />
                    <div className="min-w-0">
                      <div>Set up billing</div>
                      <p className="mt-1 text-sm font-normal text-muted-foreground">
                        Add a card to unlock your workspace and continue on pay
                        as you go.
                      </p>
                    </div>
                  </div>
                }
                disabled={paygCheckout.isPending}
                onSelect={paygCheckout.startCheckout}
              />
              <RadioCard
                value="sales"
                title={
                  <div className="flex gap-3">
                    <Icon
                      name="calendar"
                      className="mt-0.5 ml-0.5 size-5 shrink-0"
                      aria-hidden="true"
                    />
                    <div className="min-w-0">
                      <div>Contact sales</div>
                      <p className="mt-1 text-sm font-normal text-muted-foreground">
                        Talk with our team about plans, pricing, and next steps.
                      </p>
                    </div>
                  </div>
                }
                onSelect={openSalesCalendar}
              />
            </RadioCardGroup>
            {paygCheckout.error ? (
              <ErrorAlert
                error={paygCheckout.error}
                title="Checkout failed"
                className="mt-2"
              />
            ) : null}
          </div>
        </Card.Content>
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
      <BookingCalendarModal {...modalProps} />
    </AuthShell>
  );
}
