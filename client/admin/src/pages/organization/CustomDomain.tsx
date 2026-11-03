import type { JSX, ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import type { AdminCustomDomain } from "@gram/admin-client/models/components/admincustomdomain";

import { CopyValue } from "@/components/CopyValue";
import { Button } from "@/components/ui/button";
import { organizationQuery } from "@/lib/adminQueries";
import type { AdminOrganization } from "@/lib/gramAdminApi";
import { adminListCustomDomainsQuery } from "@/lib/gramAdminClient";
import { fmtDateShort } from "@/lib/utils";
import {
  DomainStatusBadge,
  DNSConfigurationCell,
  HealthCell,
  RoutingMethodBadge,
  RoutingMethodHelp,
} from "@/pages/custom-domains/CustomDomainBadges";
import {
  fmtDateTimeUtc,
  missingTargetReason,
} from "@/pages/custom-domains/customDomainModel";

function Row({
  label,
  children,
}: {
  label: ReactNode;
  children: ReactNode;
}): JSX.Element {
  return (
    <div className="grid grid-cols-1 gap-1 py-1 sm:grid-cols-[10rem_minmax(0,1fr)] sm:items-center sm:gap-3">
      <span className="text-muted-foreground text-sm">{label}</span>
      <div className="min-w-0">{children}</div>
    </div>
  );
}

export function CustomDomainRoute(): JSX.Element | null {
  const { idOrSlug } = useParams({ from: "/organizations/$idOrSlug" });
  const { data } = useQuery(organizationQuery(idOrSlug));
  if (!data) return null;
  return <CustomDomain org={data} />;
}

export function CustomDomain({ org }: { org: AdminOrganization }): JSX.Element {
  // Keyed by the organization id rather than the address, so the record opened
  // by slug and by id reads the same entry.
  const query = useQuery({
    ...adminListCustomDomainsQuery({ organizationId: org.id }),
    enabled: !!org.id,
  });

  if (query.isPending) {
    return (
      <p role="status" className="text-muted-foreground text-sm">
        Loading...
      </p>
    );
  }
  if (query.isError) {
    return (
      <p role="alert" className="text-sm">
        Unable to load the custom domain. {query.error.message}
        <Button variant="ghost" onClick={() => void query.refetch()}>
          Retry
        </Button>
      </p>
    );
  }

  // At most one live domain per organization.
  const domain = query.data.customDomains[0];
  if (!domain) {
    return (
      <p className="text-muted-foreground text-sm">
        This organization has no custom domain.
      </p>
    );
  }
  return <CustomDomainDetails domain={domain} />;
}

function CustomDomainDetails({
  domain,
}: {
  domain: AdminCustomDomain;
}): JSX.Element {
  return (
    <section className="bg-card max-w-3xl rounded-lg border">
      <div className="border-b px-5 py-2.5">
        <h5 className="text-sm font-semibold">{domain.domain}</h5>
      </div>
      <div className="p-5">
        <Row label="Status">
          <DomainStatusBadge domain={domain} />
        </Row>
        <Row
          label={
            <span className="flex items-center gap-1">
              Routing method
              <RoutingMethodHelp />
            </span>
          }
        >
          <RoutingMethodBadge method={domain.dnsRoutingMethod} />
        </Row>
        <Row label="Designated target">
          {domain.dnsTarget ? (
            <CopyValue
              label="Designated target"
              value={domain.dnsTarget}
              className="text-sm"
            />
          ) : (
            <span className="text-muted-foreground text-sm">
              {missingTargetReason(domain)}
            </span>
          )}
        </Row>
        <Row label="Target token">
          {domain.dnsTargetToken ? (
            <CopyValue
              label="Target token"
              value={domain.dnsTargetToken}
              className="text-sm"
            />
          ) : (
            <span className="text-muted-foreground text-sm">-</span>
          )}
        </Row>
        <Row label="DNS configuration">
          <DNSConfigurationCell domain={domain} />
        </Row>
        <Row label="Infrastructure">
          <HealthCell domain={domain} />
        </Row>
        <Row label="Health checked">
          <span className="text-sm">
            {fmtDateTimeUtc(domain.healthCheckedAt)}
          </span>
        </Row>
        <Row label="Registered">
          <span className="text-sm">
            {fmtDateShort(domain.createdAt.toISOString())}
          </span>
        </Row>
        <Row label="Domain ID">
          <CopyValue label="Domain ID" value={domain.id} className="text-sm" />
        </Row>
      </div>
    </section>
  );
}
