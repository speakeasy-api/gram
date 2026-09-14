import type { JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { adminListGlobalIssuerConvergenceCandidatesQuery } from "@/lib/gramAdminClient";
import { ConvergenceHelp } from "./Convergence";
export function ConvergenceSummary({
  issuerId,
}: {
  issuerId: string;
}): JSX.Element {
  const query = useQuery(
    adminListGlobalIssuerConvergenceCandidatesQuery({
      targetId: issuerId,
      limit: 50,
    }),
  );
  return (
    <section className="grid gap-2">
      <div className="flex items-center gap-1">
        <h2 className="text-base font-semibold">Convergence</h2>
        <ConvergenceHelp />
      </div>
      {query.isPending && <p role="status">Loading convergence summary…</p>}
      {query.error && (
        <p role="alert">
          Convergence summary unavailable: {query.error.message}
        </p>
      )}
      {query.data && (
        <p className="text-muted-foreground text-sm">
          {query.data.result.nextCursor
            ? `At least ${query.data.result.items.length} matching issuers; more candidates are available on subsequent pages.`
            : `${query.data.result.items.length} matching organization or project issuers.`}{" "}
          Compatibility is checked individually during review; these counts do
          not indicate migration readiness.
        </p>
      )}
      <Link
        to="/remote-session-issuers/$issuerId/convergence"
        params={{ issuerId }}
        className="text-sm underline underline-offset-4"
      >
        Review convergence candidates
      </Link>
    </section>
  );
}
