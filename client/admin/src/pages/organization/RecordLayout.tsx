import type { JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { Outlet, useParams } from "@tanstack/react-router";

import { organizationQuery } from "@/lib/adminQueries";
import { errorMessage } from "@/lib/gramAdminApi";

export function RecordLayout(): JSX.Element {
  const { idOrSlug } = useParams({ from: "/organizations/$idOrSlug" });
  const { data, isLoading, isError, error } = useQuery({
    ...organizationQuery(idOrSlug),
    enabled: !!idOrSlug,
  });

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
    <div className="flex min-h-0 flex-1 flex-col gap-6">
      <h4 className="text-[1.438rem] leading-[1.6] font-light">{data.name}</h4>
      <Outlet />
    </div>
  );
}
