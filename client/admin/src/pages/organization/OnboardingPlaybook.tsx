import { useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import type { JSX } from "react";

import { Button } from "@/components/ui/button";
import { organizationOnboardingPlaybookQuery } from "@/lib/gramAdminClient";
import { cn } from "@/lib/utils";

/**
 * The organization's assigned playbook, which the setup wizard walks, as a
 * Details row value on the Overview page: its name, linking to the Use Cases
 * & Playbooks page scoped to the organization, where staff write or assign
 * one.
 */
export function OnboardingPlaybook({
  organizationId,
}: {
  organizationId: string;
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
  // A failed refresh hides the stale name too: the row is the truth about
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
  // The name is the link: it opens the Playbooks page scoped to this
  // organization, which is where the playbook is read and changed.
  return (
    <Link
      to="/onboarding-playbooks"
      search={{ organization: organizationId }}
      className={cn(
        "text-sm underline underline-offset-4 hover:no-underline",
        !current && "text-muted-foreground",
      )}
    >
      {current ? current.name : "Not assigned"}
    </Link>
  );
}
