import type { JSX } from "react";

import { Trial } from "@/components/Trial";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { badgeTone } from "@/lib/badgeTone";
import type { AdminOrganization } from "@/lib/gramAdminApi";
import { impersonationUrl } from "@/lib/impersonation";
import { fmtDateShort } from "@/lib/utils";
import { OrganizationActions } from "@/pages/organizations/OrganizationActions";

// Ends the fact it follows rather than starting the next one, so a meta line
// that wraps never opens a line with a separator.
function Dot(): JSX.Element {
  return (
    <span
      aria-hidden="true"
      className="bg-muted-foreground/60 ml-2 inline-block size-[3px] rounded-full align-middle"
    />
  );
}

export function RecordHeader({ org }: { org: AdminOrganization }): JSX.Element {
  const gramUrl = impersonationUrl(org.slug);

  return (
    <div className="flex flex-wrap items-start justify-between gap-x-4 gap-y-3">
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <h4 className="text-[1.438rem] leading-[1.6] font-light">
            {org.name}
          </h4>
          <Badge variant="outline" className={badgeTone.neutral}>
            {org.account_type}
          </Badge>
          {/* Keyed on `none`, never on "would `Trial` draw a dash". An
              unrecognised state draws the same dash and has to keep it. */}
          {org.trial_state && org.trial_state !== "none" && <Trial org={org} />}
        </div>
        {/* Two facts and no more. The line grows when the events slice lands;
            it is not padded to fill now. */}
        <p className="text-muted-foreground flex flex-wrap items-center gap-x-2 text-sm">
          <span className="whitespace-nowrap">
            {org.account_type}
            <Dot />
          </span>
          <span className="whitespace-nowrap">
            Created {fmtDateShort(org.created_at)}
          </span>
        </p>
      </div>

      <div className="flex shrink-0 items-center gap-2">
        {/* Absent rather than dead when no app origin is configured. */}
        {gramUrl && (
          <Button asChild variant="outline" size="xs">
            <a href={gramUrl} target="_blank" rel="noreferrer">
              Open in Gram
            </a>
          </Button>
        )}
        <OrganizationActions org={org} layout="buttons" />
      </div>
    </div>
  );
}
