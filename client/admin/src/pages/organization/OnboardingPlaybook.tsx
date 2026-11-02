import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { JSX } from "react";

import { Button } from "@/components/ui/button";
import { organizationOnboardingPlaybookQuery } from "@/lib/gramAdminClient";

// Fetch by canonical ID, but preserve the address used to visit the record.
export function OnboardingPlaybook({
  organizationId,
  idOrSlug,
}: {
  organizationId: string;
  idOrSlug?: string;
}): JSX.Element {
  const assigned = useQuery({
    ...organizationOnboardingPlaybookQuery(organizationId),
    throwOnError: false,
  });
  if (assigned.isPending) {
    return (
      <span role="status" className="text-muted-foreground text-sm">
        Loading…
      </span>
    );
  }
  // A failed refresh hides the stale name too: the status is the truth about
  // what the wizard walks, so it offers the retry instead.
  if (!assigned.data || assigned.isError) {
    return (
      <div
        role="alert"
        className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm"
      >
        <span className="text-muted-foreground">
          Unable to load the playbook.
        </span>
        <Button
          variant="outline"
          size="sm"
          onClick={() => void assigned.refetch()}
        >
          Retry playbook
        </Button>
      </div>
    );
  }
  const current = assigned.data.playbook;
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
      <span className="text-muted-foreground">
        {current ? `Playbook: ${current.name}` : "No playbook assigned"}
      </span>
      <Link
        to="/organizations/$idOrSlug/onboarding"
        params={{ idOrSlug: idOrSlug ?? organizationId }}
        className="underline underline-offset-4 hover:no-underline"
      >
        Customize onboarding
      </Link>
    </div>
  );
}
