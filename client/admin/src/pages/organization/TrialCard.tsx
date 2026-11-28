import { Link } from "@tanstack/react-router";
import type { JSX } from "react";

import type {
  AdminOrganization,
  TrialCardRejectionReason,
} from "@/lib/gramAdminApi";
import { tone } from "@/lib/tone";
import { cn } from "@/lib/utils";

function dateLabel(iso: string): string {
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? "Unknown" : date.toLocaleDateString();
}

const REJECTION_LABELS: Record<TrialCardRejectionReason, string> = {
  prepaid_card: "prepaid card",
  card_used: "already started another organization's trial",
};

// The latest card the organization offered to start a self-serve trial, and
// the other organizations that offered the same card.
export function TrialCard({
  org,
}: {
  org: AdminOrganization;
}): JSX.Element | null {
  if (!org.trial_card_added_at) return null;
  const sharing = org.organizations_sharing_trial_card ?? [];
  const rejection = org.trial_card_rejection_reason;
  const added = dateLabel(org.trial_card_added_at);

  return (
    <div data-slot="organization-trial-card" className="space-y-2 py-1">
      <div className="grid grid-cols-1 gap-1 sm:grid-cols-[7.5rem_minmax(0,1fr)] sm:items-center sm:gap-3">
        <span data-slot="field-label" className="text-muted-foreground text-sm">
          Trial card
        </span>
        <span className="text-sm">
          {rejection
            ? `Rejected ${added}: ${REJECTION_LABELS[rejection]}`
            : `Added ${added}`}
        </span>
      </div>
      {sharing.length > 0 && (
        <div
          role="status"
          className={cn("rounded-md border px-3 py-2 text-sm", tone.warning)}
        >
          <p className="font-medium">
            {sharing.length === 1
              ? "Another organization offered this card"
              : `${sharing.length} other organizations offered this card`}
          </p>
          <ul className="mt-1 space-y-0.5">
            {sharing.map((other) => (
              <li key={other.id}>
                <Link
                  to="/organizations/$idOrSlug"
                  params={{ idOrSlug: other.id }}
                  className="underline underline-offset-4"
                >
                  {other.name}
                </Link>{" "}
                <span className="opacity-80">
                  ({other.slug}) ·{" "}
                  {other.rejection_reason
                    ? `rejected: ${REJECTION_LABELS[other.rejection_reason]}`
                    : "started its trial"}{" "}
                  · offered {dateLabel(other.card_added_at)}
                </span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
