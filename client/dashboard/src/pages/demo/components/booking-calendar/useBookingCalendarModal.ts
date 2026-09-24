import { useCallback, useState } from "react";
import type { BookingCalendarModalOptions } from "./BookingCalendarModal";
import type { BookingCalendarModalProps } from "./BookingCalendarModal";

type UseBookingCalendarModalResult = {
  openBookingCalendar: (options?: BookingCalendarModalOptions) => void;
  modalProps: BookingCalendarModalProps;
};

export function useBookingCalendarModal(): UseBookingCalendarModalResult {
  const [open, setOpen] = useState(false);
  const [options, setOptions] = useState<BookingCalendarModalOptions>({});

  const openBookingCalendar = useCallback(
    (nextOptions: BookingCalendarModalOptions = {}) => {
      setOptions(nextOptions);
      setOpen(true);
    },
    [],
  );

  const handleOpenChange = useCallback(
    (nextOpen: boolean) => {
      setOpen(nextOpen);
      if (!nextOpen) options.onClose?.();
    },
    [options],
  );

  return {
    openBookingCalendar,
    modalProps: { ...options, open, onOpenChange: handleOpenChange },
  };
}
