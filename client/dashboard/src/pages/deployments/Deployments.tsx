import { Page } from "@/components/page-layout";
import { Heading } from "@/components/ui/heading";
import { useRoutes } from "@/routes";
import { useListDeploymentsSuspense } from "@gram/client/react-query";
import { Icon, Table, TableProps } from "@speakeasy-api/moonshine";
import { Suspense } from "react";
import { Outlet } from "react-router";

export default function DeploymentsPage() {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Suspense fallback={<div>Loading...</div>}>
          <DeploymentsTable />
        </Suspense>
      </Page.Body>
    </Page>
  );
}

export function DeploymentsRoot() {
  return <Outlet />;
}

type DeploymentSummary = {
  id: string;
  status: string;
  createdAt: Date;
  assetCount: number;
  toolCount: number;
};

const columns: TableProps<DeploymentSummary>["columns"] = [
  {
    key: "id",
    header: "ID",
    render: (row) => {
      let icon: React.ReactNode = null;
      switch (row.status) {
        case "completed":
          icon = <Icon name="check" className="text-success" />;
          break;
        case "failed":
          icon = <Icon name="x" className="text-destructive" />;
          break;
        default:
          icon = <Icon name="circle-dashed" className="text-warning" />;
          break;
      }

      const createdAt = relativeTime(row.createdAt);

      return (
        <div>
          <p className="inline-flex items-center gap-1 font-mono">
            {icon}
            <DeploymentLink id={row.id} />
          </p>
          <p className="text-muted-foreground text-sm ps-5">{createdAt}</p>
        </div>
      );
    },
    width: "auto",
  },
  {
    key: "assetCount",
    header: "Assets",
    render: (row) => row.assetCount,
    width: "auto",
  },
  {
    key: "toolCount",
    header: "Tools",
    render: (row) => row.toolCount,
    width: "1fr",
  },
];

function DeploymentsTable() {
  const { data: res } = useListDeploymentsSuspense();

  return (
    <>
      <Heading variant="h2">Recent Deployments</Heading>

      <Table<DeploymentSummary>
        columns={columns}
        rowKey={(row) => row.id}
        data={res.items ?? []}
      />
    </>
  );
}

export function DeploymentLink({ id }: { id: string }) {
  const routes = useRoutes();
  return (
    <routes.deployments.deployment.Link params={[id]}>
      {id}
    </routes.deployments.deployment.Link>
  );
}

function relativeTime(date: Date): string {
  const now = Date.now();
  const createdAt = date.getTime();
  const diffMs = now - createdAt;

  const seconds = Math.floor(diffMs / 1000);
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);

  const rtf = new Intl.RelativeTimeFormat("en", { numeric: "auto" });

  if (days > 0) {
    return rtf.format(-days, "day");
  } else if (hours > 0) {
    return rtf.format(-hours, "hour");
  } else if (minutes > 0) {
    return rtf.format(-minutes, "minute");
  } else {
    return rtf.format(-seconds, "second");
  }
}
