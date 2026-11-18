import { DETAIL_HEADER_CONTROL_BUTTON } from "@/components/detail/detail-frame";
import { ChevronRight } from "lucide-react";
import { useMemo } from "react";
import { Link } from "react-router";
import { connectionHealthNow, useSessionActivity } from "./api";
import { presetWindow } from "./filters";
import { formatSessionLength } from "./format";

/** The header's one Session duration readout: the 30-day average and the setting. */
export function SessionDurationControl({
  providerId,
  configuredHours,
  href,
}: {
  providerId: string;
  configuredHours: number;
  href: string;
}): JSX.Element {
  const window = useMemo(() => presetWindow("30d", connectionHealthNow()), []);
  const activity = useSessionActivity(providerId, window);
  const average = activity.data?.averageSessionSeconds;
  const setTo = formatSessionLength(configuredHours * 3_600);
  const label =
    average === undefined
      ? `Set to ${setTo}`
      : `Avg ${formatSessionLength(average)} · set to ${setTo}`;
  return (
    <Link
      to={href}
      aria-label={`${label}. The average covers sessions that ended in the last 30 days. Change session duration in Settings`}
      title="Average of sessions that ended in the last 30 days"
      className={DETAIL_HEADER_CONTROL_BUTTON}
    >
      <span className="min-w-0 flex-1 truncate text-left">{label}</span>
      <ChevronRight className="text-muted-foreground size-3.5 shrink-0" />
    </Link>
  );
}
