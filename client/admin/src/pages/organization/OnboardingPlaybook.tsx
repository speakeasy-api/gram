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
  const current = assigned.data?.playbook;
  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
      {assigned.isPending ? (
        <span role="status" className="text-muted-foreground">
          Loading…
        </span>
      ) : !assigned.data || assigned.isError ? (
        <div
          role="alert"
          className="flex flex-wrap items-center gap-x-3 gap-y-1"
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
      ) : (
        <span className="text-muted-foreground">
          {current ? `Playbook: ${current.name}` : "No playbook assigned"}
        </span>
      )}
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
