import type { JSX } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, useNavigate, useSearch } from "@tanstack/react-router";
import { createColumnHelper, useTable } from "@tanstack/react-table";
import { RouteIcon } from "lucide-react";
import type { AdminCustomDomain } from "@gram/admin-client/models/components/admincustomdomain";

import { CopyValue } from "@/components/CopyValue";
import {
  dataTableFeatures,
  DataTable as Table,
  type DataTableFeatures,
} from "@/components/data-table";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { adminListCustomDomainsQuery } from "@/lib/gramAdminClient";
import { fmtDateShort } from "@/lib/utils";

import {
  DomainStatusBadge,
  DNSConfigurationCell,
  HealthCell,
  RoutingMethodBadge,
  RoutingMethodHelp,
} from "./CustomDomainBadges";
import {
  fmtDateTimeUtc,
  missingTargetReason,
  ROUTING_FILTER_LABELS,
  ROUTING_METHODS,
} from "./customDomainModel";
import type { CustomDomainsSearch } from "./customDomainsSearch";

// The filter value that shows every routing method.
const ALL = "all";

const empty: AdminCustomDomain[] = [];

const column = createColumnHelper<DataTableFeatures, AdminCustomDomain>();

const COLUMNS = column.columns([
  column.accessor("domain", {
    header: "Domain",
    meta: { cellClassName: "whitespace-normal break-all min-w-48" },
    cell: ({ row }) => (
      <span className="text-sm font-medium">{row.original.domain}</span>
    ),
  }),
  column.display({
    id: "organization",
    header: "Organization",
    meta: { cellClassName: "whitespace-normal" },
    cell: ({ row }) => (
      <div>
        <Link
          to="/organizations/$idOrSlug/custom-domain"
          params={{
            idOrSlug:
              row.original.organizationSlug || row.original.organizationId,
          }}
          className="text-sm underline-offset-4 hover:underline focus-visible:underline"
        >
          {row.original.organizationName}
        </Link>
        <p className="text-muted-foreground text-xs">
          {row.original.organizationSlug}
        </p>
      </div>
    ),
  }),
  column.display({
    id: "status",
    header: "Status",
    cell: ({ row }) => <DomainStatusBadge domain={row.original} />,
  }),
  column.display({
    id: "routing",
    header: () => (
      <span className="flex items-center gap-1">
        Routing
        <RoutingMethodHelp />
      </span>
    ),
    cell: ({ row }) => (
      <RoutingMethodBadge method={row.original.dnsRoutingMethod} />
    ),
  }),
  column.display({
    id: "target",
    header: "Designated target",
    // Whatever width the others leave, so a long target truncates here rather
    // than pushing the table out of its border.
    meta: { cellClassName: "max-w-0 w-full min-w-48" },
    cell: ({ row }) =>
      row.original.dnsTarget ? (
        <CopyValue
          label={`${row.original.domain} designated target`}
          value={row.original.dnsTarget}
        />
      ) : (
        // The target starts with the token, so the token only needs its own
        // line when there is no target to show it in.
        <div className="whitespace-normal">
          {row.original.dnsTargetToken && (
            <p className="font-mono text-xs">{row.original.dnsTargetToken}</p>
          )}
          <p className="text-muted-foreground text-xs">
            {missingTargetReason(row.original)}
          </p>
        </div>
      ),
  }),
  column.display({
    id: "dns",
    header: "DNS configuration",
    meta: { cellClassName: "min-w-40" },
    cell: ({ row }) => <DNSConfigurationCell domain={row.original} />,
  }),
  column.display({
    id: "health",
    header: "Infrastructure",
    meta: { cellClassName: "min-w-32" },
    cell: ({ row }) => (
      <div>
        <HealthCell domain={row.original} />
        {row.original.healthCheckedAt && (
          <p className="text-muted-foreground text-xs">
            Checked {fmtDateTimeUtc(row.original.healthCheckedAt)}
          </p>
        )}
      </div>
    ),
  }),
  column.display({
    id: "created",
    header: "Registered",
    cell: ({ row }) => (
      <span className="text-sm">
        {fmtDateShort(row.original.createdAt.toISOString())}
      </span>
    ),
  }),
]);

function emptyMessage(filtered: boolean): string {
  return filtered
    ? "No custom domains use this routing method."
    : "No organization has a custom domain.";
}

export function CustomDomains(): JSX.Element {
  const { routing } = useSearch({ from: "/custom-domains" });
  const navigate = useNavigate({ from: "/custom-domains" });
  const query = useQuery(
    adminListCustomDomainsQuery({ dnsRoutingMethod: routing }),
  );
  const domains = query.data?.customDomains ?? empty;

  const table = useTable({
    features: dataTableFeatures,
    columns: COLUMNS,
    data: domains,
    getRowId: (domain) => domain.id,
  });
  const rows = table.getRowModel().rows;

  const filterBy = (next: CustomDomainsSearch) => {
    void navigate({ search: next, replace: true });
  };

  return (
    <div className="space-y-6 p-6">
      <div className="space-y-1">
        <h1 className="text-2xl font-semibold">Custom domains</h1>
        <p className="text-muted-foreground text-sm">
          Inspect designated targets and the latest public DNS observations. DNS
          and infrastructure checks do not prove endpoint availability.
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <Select
          value={routing ?? ALL}
          onValueChange={(value) =>
            filterBy({
              routing: ROUTING_METHODS.find((method) => method === value),
            })
          }
        >
          <SelectTrigger
            size="sm"
            aria-label="Routing method"
            className="bg-card"
          >
            <RouteIcon />
            <SelectValue />
          </SelectTrigger>
          <SelectContent position="popper" align="start" className="min-w-56">
            <SelectItem value={ALL}>All routing methods</SelectItem>
            {ROUTING_METHODS.map((method) => (
              <SelectItem key={method} value={method}>
                {ROUTING_FILTER_LABELS[method]}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {query.isSuccess && (
          <span className="text-muted-foreground text-sm" aria-live="polite">
            {domains.length === 1 ? "1 domain" : `${domains.length} domains`}
          </span>
        )}
      </div>

      {query.error && (
        <p role="alert" className="text-sm">
          Unable to load custom domains. {query.error.message}
          <Button variant="ghost" onClick={() => void query.refetch()}>
            Retry
          </Button>
        </p>
      )}

      <div className="overflow-clip rounded-lg border">
        <Table cellPadding="condensed">
          <Table.Header table={table} />
          <Table.Body>
            {rows.length === 0 ? (
              <Table.NoResultsMessage>
                <span className="text-muted-foreground text-sm">
                  {query.isPending
                    ? "Loading..."
                    : query.isError
                      ? "Unable to load custom domains"
                      : emptyMessage(routing !== undefined)}
                </span>
              </Table.NoResultsMessage>
            ) : (
              rows.map((row) => <Table.Row key={row.id} row={row} />)
            )}
          </Table.Body>
        </Table>
      </div>
    </div>
  );
}
