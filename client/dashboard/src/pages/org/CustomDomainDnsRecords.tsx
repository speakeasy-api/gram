import type { CustomDomain } from "@gram/client/models/components/customdomain.js";
import type { ReactNode } from "react";
import { useState } from "react";

import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { Text } from "@/components/ui/Text";

type RecordType = "cname" | "a";

type DnsRecordsDomain = Pick<
  CustomDomain,
  "domain" | "organizationId" | "dnsTarget" | "suggestedRecordType"
>;

function RecordValue({ value, label }: { value: string; label: string }) {
  return (
    <div className="bg-muted mt-2 flex items-center gap-2 p-3">
      <code className="min-w-0 flex-1 break-all">{value}</code>
      <CopyButton
        text={value}
        size="sm"
        tooltip={`Copy ${label}`}
        className="shrink-0"
      />
    </div>
  );
}

function StepHeading({ children }: { children: ReactNode }) {
  return (
    <Text variant="body" className="mb-2 block text-lg font-extrabold">
      {children}
    </Text>
  );
}

/**
 * The DNS records a customer adds for a registered custom domain: the routing
 * record (the domain's designated CNAME target when it has one, otherwise the
 * shared CNAME or the static A records) and the ownership TXT record.
 *
 * Apex detection comes from the server's suggested record type, which is only
 * "a" when static A records are configured.
 */
export function CustomDomainDnsRecords({
  domain,
  aRecords,
  sharedCnameTarget,
}: {
  domain: DnsRecordsDomain;
  aRecords: string[];
  sharedCnameTarget: string;
}): JSX.Element {
  const designatedTarget = domain.dnsTarget;
  const isApex = domain.suggestedRecordType === "a" && aRecords.length > 0;
  // A designated target works at the apex through CNAME flattening, so it is
  // the default everywhere; without one the server's suggestion stands.
  const defaultRecordType: RecordType =
    designatedTarget || !isApex ? "cname" : "a";
  const [recordTypeOverride, setRecordTypeOverride] =
    useState<RecordType | null>(null);
  const recordType = recordTypeOverride ?? defaultRecordType;

  const cnameLabel = designatedTarget
    ? isApex
      ? "CNAME flattening (recommended)"
      : "CNAME (recommended)"
    : "CNAME (subdomain)";
  const aLabel = designatedTarget ? "A records" : "A record (apex domain)";

  const txtName = `_gram.${domain.domain}`;
  const txtValue = `gram-domain-verify=${domain.domain},${domain.organizationId}`;

  return (
    <div className="space-y-6">
      <div>
        <StepHeading>Step 1</StepHeading>
        {aRecords.length > 0 && (
          <div className="mb-2 flex gap-1">
            <Button
              variant={recordType === "cname" ? "secondary" : "tertiary"}
              size="sm"
              onClick={() => setRecordTypeOverride("cname")}
            >
              {cnameLabel}
            </Button>
            <Button
              variant={recordType === "a" ? "secondary" : "tertiary"}
              size="sm"
              onClick={() => setRecordTypeOverride("a")}
            >
              {aLabel}
            </Button>
          </div>
        )}
        {recordType === "a" ? (
          <>
            <Text variant="body" className="text-muted-foreground mb-2">
              {designatedTarget && isApex ? (
                <>
                  If your DNS provider doesn&apos;t support CNAME flattening,
                  create A records for{" "}
                  <span className="font-mono break-all">{domain.domain}</span>{" "}
                  (the zone root, often written as{" "}
                  <span className="font-mono">@</span>) pointing to these
                  addresses. They&apos;re less flexible: if our addresses
                  change, you&apos;ll need to update these records.
                </>
              ) : designatedTarget ? (
                <>
                  Create A records for{" "}
                  <span className="font-mono break-all">{domain.domain}</span>{" "}
                  pointing to these addresses. They&apos;re less flexible than a
                  CNAME: if our addresses change, you&apos;ll need to update
                  these records.
                </>
              ) : (
                <>
                  Create an A record for{" "}
                  <span className="font-mono break-all">{domain.domain}</span>{" "}
                  (the zone root, often written as{" "}
                  <span className="font-mono">@</span>) pointing to:
                </>
              )}
            </Text>
            {aRecords.map((ip) => (
              <RecordValue key={ip} value={ip} label="A record value" />
            ))}
          </>
        ) : (
          <>
            <Text variant="body" className="text-muted-foreground mb-2">
              {designatedTarget && isApex ? (
                <>
                  Point{" "}
                  <span className="font-mono break-all">{domain.domain}</span>{" "}
                  at the designated domain below so we can move your domain
                  later without any DNS changes on your side. A standard CNAME
                  can&apos;t sit at the zone root, but many DNS providers
                  support one there as <strong>CNAME flattening</strong>, or as
                  an <strong>ALIAS</strong> or <strong>ANAME</strong> record.
                  Create one for <span className="font-mono">@</span> pointing
                  to:
                </>
              ) : designatedTarget ? (
                <>
                  Create a CNAME record for{" "}
                  <span className="font-mono break-all">{domain.domain}</span>{" "}
                  pointing to its designated domain:
                </>
              ) : (
                <>
                  Create a CNAME record for{" "}
                  <span className="font-mono break-all">{domain.domain}</span>{" "}
                  pointing to the following:
                </>
              )}
            </Text>
            <RecordValue
              value={designatedTarget || sharedCnameTarget}
              label="CNAME value"
            />
          </>
        )}
        <Text variant="body" className="text-muted-foreground mt-2 text-sm">
          DNS changes can take a while to propagate, with some providers several
          hours. We re-check every few minutes (at most every 5 minutes apart)
          for up to 24 hours, and you can trigger a check any time with “Check
          now”.
        </Text>
      </div>
      <div>
        <StepHeading>Step 2</StepHeading>
        <Text variant="body" className="text-muted-foreground mb-2">
          Create a TXT record at{" "}
          <span className="font-mono break-all">{txtName}</span> with the
          following value:
        </Text>
        <RecordValue value={txtValue} label="TXT value" />
      </div>
    </div>
  );
}

export function RecommendedDnsUpdateAlert({
  domain,
  actions,
}: {
  domain: Pick<CustomDomain, "domain" | "dnsTarget">;
  actions?: ReactNode;
}): JSX.Element | null {
  if (!domain.dnsTarget) {
    return null;
  }
  return (
    <Alert
      variant="info"
      dismissible={false}
      className="mt-4"
      title="Recommended update"
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0 flex-1 space-y-1">
          <Text variant="body" className="text-sm">
            Update the CNAME record for{" "}
            <span className="font-mono break-all">{domain.domain}</span> to
            point to its designated domain. This lets us move your domain
            without DNS changes in future.
          </Text>
          <RecordValue value={domain.dnsTarget} label="CNAME value" />
          <Text variant="body" className="text-sm opacity-80">
            Your domain keeps working while the change propagates. Check again
            afterwards to clear this notice.
          </Text>
        </div>
        {actions}
      </div>
    </Alert>
  );
}
