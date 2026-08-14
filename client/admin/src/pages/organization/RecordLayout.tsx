import { useMemo, useState, type JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { Outlet, useParams } from "@tanstack/react-router";

import { useAnnouncer } from "@/hooks/use-announcer";
import { organizationQuery } from "@/lib/adminQueries";
import { errorMessage } from "@/lib/gramAdminApi";
import {
  WriteReportProvider,
  type WriteReporter,
} from "@/pages/organizations/OrganizationActions";

import { RecordHeader } from "./RecordHeader";
import { TrialCallout } from "./TrialCallout";

export function RecordLayout(): JSX.Element {
  const { idOrSlug } = useParams({ from: "/organizations/$idOrSlug" });
  const { data, isError, error } = useQuery({
    ...organizationQuery(idOrSlug),
    enabled: !!idOrSlug,
  });

  const [failure, setFailure] = useState<string | null>(null);
  const { announce, announced } = useAnnouncer();

  // The record's actions report through context, whose default is a silent
  // no-op: without a reporter every write on this page succeeds and announces
  // nothing.
  const reporter = useMemo<WriteReporter>(
    () => ({ announce, showFailure: setFailure }),
    [announce],
  );

  // `data`, not the status: React Query keeps the last good record when a
  // refetch over it fails, and `AppSidebar` branches on the same `data`, so
  // neither column takes away a record the other is still drawing. A record
  // that never arrived gets no chrome and no outlet, which would strand the
  // operator with a back link; a paused query is pending rather than failed
  // and has no error to name.
  if (!data) {
    return (
      <span className="text-muted-foreground text-sm">
        {isError ? `Error: ${errorMessage(error)}` : "Loading..."}
      </span>
    );
  }

  return (
    <WriteReportProvider value={reporter}>
      <div className="flex min-h-0 flex-1 flex-col gap-6">
        <RecordHeader org={data} />
        <TrialCallout org={data} />
        {failure && <p className="text-destructive text-sm">{failure}</p>}
        {/* Polite, and load-bearing. Radix marks the rest of the document
            aria-hidden while a dialog is open and exempts live regions by name,
            so a write reported from inside a dialog reaches a screen reader
            only through this. */}
        <p aria-live="polite" className="sr-only">
          {announced}
        </p>
        <Outlet />
      </div>
    </WriteReportProvider>
  );
}
