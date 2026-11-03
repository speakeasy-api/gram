import type { JSX } from "react";
import { InfoIcon } from "lucide-react";
import type { AdminCustomDomain } from "@gram/admin-client/models/components/admincustomdomain";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { badgeTone } from "@/lib/badgeTone";

import {
  A_RECORD_CAVEAT,
  domainStatusLabel,
  dnsConfigurationLabel,
  fmtDateTimeUtc,
  healthLabel,
  routingMethodLabel,
  type Label,
} from "./customDomainModel";

function ToneBadge({ label, tone }: Label): JSX.Element {
  return (
    <Badge variant="outline" className={badgeTone[tone]}>
      {label}
    </Badge>
  );
}

export function RoutingMethodBadge({
  method,
}: {
  method: AdminCustomDomain["dnsRoutingMethod"];
}): JSX.Element {
  return <ToneBadge {...routingMethodLabel(method)} />;
}

export function DomainStatusBadge({
  domain,
}: {
  domain: Pick<AdminCustomDomain, "verified" | "activated">;
}): JSX.Element {
  return <ToneBadge {...domainStatusLabel(domain)} />;
}

// The status, and under it the reason the last check gave when it failed.
export function HealthCell({
  domain,
}: {
  domain: Pick<AdminCustomDomain, "healthStatus" | "healthIssue">;
}): JSX.Element {
  return (
    <div className="grid gap-1">
      <ToneBadge {...healthLabel(domain.healthStatus)} />
      {domain.healthIssue && (
        <p className="text-muted-foreground text-xs whitespace-normal">
          {domain.healthIssue}
        </p>
      )}
    </div>
  );
}

// What each routing method means, kept beside the label rather than in a
// paragraph above the table. The A record caveat is the one an operator needs
// before reading an apex domain as an outreach candidate.
export function RoutingMethodHelp(): JSX.Element {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          type="button"
          variant="ghost"
          size="icon-xs"
          aria-label="About routing methods"
        >
          <InfoIcon />
        </Button>
      </TooltipTrigger>
      <TooltipContent className="max-w-sm">
        <p>
          Per-domain CNAME: points at its designated target. Shared CNAME: still
          points at the old shared target, so it is a candidate for outreach.
        </p>
        <p className="mt-2">{A_RECORD_CAVEAT}</p>
      </TooltipContent>
    </Tooltip>
  );
}

export function DNSConfigurationCell({
  domain,
}: {
  domain: Pick<AdminCustomDomain, "dnsConfigurationStatus" | "dnsCheckedAt">;
}): JSX.Element {
  return (
    <div className="grid gap-1">
      <ToneBadge {...dnsConfigurationLabel(domain.dnsConfigurationStatus)} />
      {domain.dnsCheckedAt && (
        <p className="text-muted-foreground text-xs">
          Checked {fmtDateTimeUtc(domain.dnsCheckedAt)}
        </p>
      )}
    </div>
  );
}
