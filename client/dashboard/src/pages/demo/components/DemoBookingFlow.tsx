import { LockoutPaygCheckoutPanel } from "@/components/billing/lockout-payg-checkout-panel";
import { BookingCalendar } from "./booking-calendar/BookingCalendar";
import type { BookingFormDefaults } from "./booking-calendar/BookingCalendar";

const DEFAULT_INTRO = (
  <div className="text-center mb-4">
    <p className="text-[16px] tracking-[0.0025em]">
      Looks like your company is new to Speakeasy.
    </p>
    <p className="mt-1.5 text-sm tracking-[0.0025em] text-(--muted-strong)">
      Book time with our team to activate your account and get started.
    </p>
  </div>
);

export function DemoBookingFlow({
  intro = DEFAULT_INTRO,
  eventLabel = "AI transformation — 30 min",
  formDefaults,
}: {
  intro?: React.ReactNode;
  eventLabel?: string;
  formDefaults?: BookingFormDefaults;
} = {}): JSX.Element {
  return (
    <div className="flex w-full flex-col items-center gap-2">
      {intro}
      <LockoutPaygCheckoutPanel />
      <BookingCalendar eventLabel={eventLabel} formDefaults={formDefaults} />
    </div>
  );
}
