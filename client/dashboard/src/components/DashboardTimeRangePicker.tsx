import {
  TimeRangePicker as ElementsTimeRangePicker,
  type TimeRangePickerProps,
} from "@/elements";
import { useSession } from "@/contexts/Auth";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { getServerURL } from "@/lib/utils";

/**
 * Dashboard wrapper around the Elements `TimeRangePicker`.
 *
 * The picker's natural-language ("type any date") parsing POSTs to
 * `/chat/completions`, which authenticates from request headers — NOT cookies —
 * and requires BOTH `Gram-Session` and `Gram-Project`. The bare Elements
 * component cannot reach dashboard auth or route context, so both are injected
 * here: the session token from `useSession()` and the request project slug
 * from `useProjectSlugForRequests()` — which falls back to the default project
 * on org-scoped pages like Billing, matching every other SDK request (callers
 * may still pass `projectSlug` explicitly to override). Missing either header
 * 401s and parsing silently no-ops.
 *
 * Use this wrapper anywhere in the dashboard instead of importing
 * `TimeRangePicker` directly from `@/elements`.
 */
export function TimeRangePicker(props: TimeRangePickerProps): JSX.Element {
  const { session } = useSession();
  const requestProjectSlug = useProjectSlugForRequests();
  return (
    <ElementsTimeRangePicker
      {...props}
      projectSlug={props.projectSlug ?? requestProjectSlug}
      apiUrl={getServerURL()}
      authHeaders={session ? { "Gram-Session": session } : undefined}
    />
  );
}
