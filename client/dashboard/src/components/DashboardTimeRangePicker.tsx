import {
  TimeRangePicker as ElementsTimeRangePicker,
  type TimeRangePickerProps,
} from "@gram-ai/elements";
import { useSession } from "@/contexts/Auth";
import { getServerURL } from "@/lib/utils";

/**
 * Dashboard wrapper around the Elements `TimeRangePicker`.
 *
 * The picker's natural-language ("type any date") parsing POSTs to
 * `/chat/completions`, which authenticates from request headers — NOT cookies.
 * Every other dashboard caller of that endpoint (e.g. the playground's
 * `useModel`) sends the `Gram-Session` token explicitly; the bare Elements
 * component cannot reach dashboard auth context, so it must be injected here.
 * Without it the request 401s and parsing silently no-ops.
 *
 * Use this wrapper anywhere in the dashboard instead of importing
 * `TimeRangePicker` directly from `@gram-ai/elements`.
 */
export function TimeRangePicker(props: TimeRangePickerProps) {
  const { session } = useSession();
  return (
    <ElementsTimeRangePicker
      {...props}
      apiUrl={getServerURL()}
      authHeaders={session ? { "Gram-Session": session } : undefined}
    />
  );
}
