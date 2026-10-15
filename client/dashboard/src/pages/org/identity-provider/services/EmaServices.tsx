import { Link } from "react-router";

import { InlineEmptyState } from "@/components/inline-empty-state";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Heading } from "@/components/ui/Heading";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { type Column, Table } from "@/components/ui/Table";
import { Text } from "@/components/ui/Text";

import { emaServiceHref } from "../tabs";
import { DEFAULT_ACCEPTS_RULE } from "./emaServiceView";
import { ProviderTrust } from "./ProviderTrust";
import {
  serversSummary,
  useEmaServices,
  type ServiceRow,
} from "./useEmaServices";

/**
 * The services identity chaining reaches, one row each: which identity
 * providers vouch to it, whether it has accepted them, and how many servers
 * go through it. The place an admin does the per-service trust step once.
 */
export function EmaServices(): JSX.Element {
  const services = useEmaServices();
  return (
    <section
      className="flex min-w-0 flex-col gap-4"
      aria-label="Connected services"
    >
      <div className="flex flex-col gap-1">
        <Heading variant="h3">Connected services</Heading>
        <Text muted small>
          Each service your MCP servers reach has to trust an identity provider
          above before Enterprise Managed Authorization works. Do that once per
          service here. {DEFAULT_ACCEPTS_RULE}
        </Text>
      </div>
      {services.isPending ? (
        <SkeletonTable />
      ) : services.isError ? (
        <Alert variant="error">
          <Text small>Couldn’t load services. Try again.</Text>
          <Button
            variant="secondary"
            size="sm"
            disabled={services.isFetching}
            onClick={services.refetch}
          >
            Retry
          </Button>
        </Alert>
      ) : services.rows.length === 0 ? (
        <InlineEmptyState
          icon="link"
          heading="No services yet"
          description="Turn on Enterprise Managed Authorization on an MCP server and the service behind it shows up here."
        />
      ) : (
        <ServicesTable rows={services.rows} />
      )}
    </section>
  );
}

function ServicesTable({ rows }: { rows: ServiceRow[] }): JSX.Element {
  const columns: Column<ServiceRow>[] = [
    {
      key: "service",
      header: "Service",
      render: (row) => (
        <div className="min-w-0">
          <Link
            to={emaServiceHref(row.vendorIssuer)}
            className="block truncate font-medium underline-offset-4 hover:underline"
          >
            {row.name}
          </Link>
          <Text muted small className="truncate">
            {row.vendorIssuer}
          </Text>
        </div>
      ),
    },
    {
      key: "providers",
      header: "Trusts",
      width: "1fr",
      render: (row) => (
        <ul
          aria-label={`${row.name} identity providers`}
          className="min-w-0 space-y-1"
        >
          {row.providers.map((provider) => (
            <li key={provider.key}>
              <ProviderTrust
                provider={provider}
                isDefaultOrder={row.isDefaultOrder}
              />
            </li>
          ))}
        </ul>
      ),
    },
    {
      key: "servers",
      header: "Servers",
      width: "150px",
      render: (row) => <Text small>{serversSummary(row)}</Text>,
    },
    {
      key: "manage",
      header: "",
      width: "100px",
      render: (row) => (
        <Button asChild variant="secondary" size="sm">
          <Link to={emaServiceHref(row.vendorIssuer)}>Manage</Link>
        </Button>
      ),
    },
  ];
  return <Table columns={columns} data={rows} rowKey={(row) => row.key} />;
}
