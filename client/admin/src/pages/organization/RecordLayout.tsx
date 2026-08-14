import { useMemo, useState, type JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { Outlet, useParams } from "@tanstack/react-router";

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
  const { data, isLoading, isError, error } = useQuery({
    ...organizationQuery(idOrSlug),
    enabled: !!idOrSlug,
  });

  const [failure, setFailure] = useState<string | null>(null);
  const [announcement, setAnnouncement] = useState("");

  // The record's actions report through context, whose default is a silent
  // no-op: without a reporter every write on this page succeeds and announces
  // nothing.
  const reporter = useMemo<WriteReporter>(
    () => ({ announce: setAnnouncement, showFailure: setFailure }),
    [],
  );

  if (isLoading) {
    return <span className="text-muted-foreground text-sm">Loading...</span>;
  }

  // No chrome and no outlet for a record that failed to load. A contextual nav
  // and four views over nothing strand the operator with a back link.
  if (isError || !data) {
    return (
      <span className="text-muted-foreground text-sm">
        Error: {errorMessage(error)}
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
          {announcement}
        </p>
        <Outlet />
      </div>
    </WriteReportProvider>
  );
}
