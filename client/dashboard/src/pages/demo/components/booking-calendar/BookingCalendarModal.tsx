import { Dialog } from "@/components/ui/Dialog";
import { BookingCalendar } from "./BookingCalendar";
import type { BookingCalendarProps } from "./BookingCalendar";

export type BookingCalendarModalOptions = BookingCalendarProps & {
  footer?: React.ReactNode;
  onClose?: () => void;
};

export type BookingCalendarModalProps = BookingCalendarModalOptions & {
  open: boolean;
  onOpenChange: (open: boolean) => void;
};

export function BookingCalendarModal({
  open,
  onOpenChange,
  eventLabel,
  formDefaults,
  telemetrySource,
  footer,
}: BookingCalendarModalProps): JSX.Element {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="auth-brand w-full max-w-4xl gap-0 overflow-hidden p-0">
        <Dialog.Title className="sr-only">Book a call</Dialog.Title>
        <BookingCalendar
          eventLabel={eventLabel}
          formDefaults={formDefaults}
          telemetrySource={telemetrySource}
        />
        {footer ? (
          <Dialog.Footer className="border-t p-4">{footer}</Dialog.Footer>
        ) : null}
      </Dialog.Content>
    </Dialog>
  );
}
