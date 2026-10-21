import { useEffect, useState, type JSX } from "react";
import { formatRemaining } from "./device-enrollment";

/**
 * Time left until `to`, ticking each second. Tabular figures so the width
 * does not jump as the digits change.
 */
export function Countdown({
  to,
  expiredLabel = "expired",
}: {
  to: Date;
  expiredLabel?: string;
}): JSX.Element {
  const [now, setNow] = useState(() => Date.now());
  const left = to.getTime() - now;
  const expired = left <= 0;
  useEffect(() => {
    if (expired) return;
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [expired]);
  return (
    <time dateTime={to.toISOString()} className="tabular-nums">
      {expired ? expiredLabel : `expires in ${formatRemaining(left)}`}
    </time>
  );
}
