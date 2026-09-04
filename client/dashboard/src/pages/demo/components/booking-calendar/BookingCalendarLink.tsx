import { Button } from "@/components/ui/Button";
import { useSession } from "@/contexts/Auth";
import { useTrialNow } from "@/hooks/useTrialNow";
import { cn } from "@/lib/utils";
import { getGateCopy } from "@/pages/demo/upgrade-gate-copy";
import type { ComponentPropsWithoutRef } from "react";
import type {
  BookingFormDefaults,
  BookingTelemetrySource,
} from "./BookingCalendar";
import { BookingCalendarModal } from "./BookingCalendarModal";
import { useBookingCalendarModal } from "./useBookingCalendarModal";

const SALES_EMAIL = "sales@speakeasy.com";

type BookingCalendarLinkProps = Omit<
  ComponentPropsWithoutRef<"button">,
  "onClick" | "type"
> & {
  eventLabel?: string;
  formDefaults?: BookingFormDefaults;
  telemetrySource?: BookingTelemetrySource;
};

export function BookingCalendarLink({
  children,
  className,
  eventLabel = "Upgrade Trial — 30 min",
  formDefaults,
  telemetrySource = "trial_upgrade",
  ...props
}: BookingCalendarLinkProps): JSX.Element {
  const session = useSession();
  const now = useTrialNow(session.trial);
  const { openBookingCalendar, modalProps } = useBookingCalendarModal();

  const handleClick = () => {
    const copy = getGateCopy(session.trial, now);
    openBookingCalendar({
      eventLabel,
      formDefaults: formDefaults ?? { source: copy.source, notes: copy.notes },
      telemetrySource,
      footer: (
        <Button variant="secondary" size="md" href={`mailto:${SALES_EMAIL}`}>
          Email {SALES_EMAIL}
        </Button>
      ),
    });
  };

  return (
    <>
      <button
        {...props}
        type="button"
        aria-haspopup="dialog"
        aria-expanded={modalProps.open}
        onClick={handleClick}
        className={cn(
          "cursor-pointer text-left underline-offset-2 hover:underline",
          className,
        )}
      >
        {children}
      </button>
      <BookingCalendarModal {...modalProps} />
    </>
  );
}
