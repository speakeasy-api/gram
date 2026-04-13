import { Page } from "@/components/page-layout";
import { Heading } from "@/components/ui/heading";
import { useRoutes } from "@/routes";
import { useListDeploymentsSuspense } from "@gram/client/react-query";
import {
  Badge,
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Icon,
  Table,
  TableProps,
} from "@speakeasy-api/moonshine";
import { Suspense, useState } from "react";
import { Outlet } from "react-router";
import { DeploymentsEmptyState } from "./DeploymentsEmptyState";
import { useActiveDeployment } from "./useActiveDeployment";
import { useRedeployDeployment } from "./useRedeployDeployment";

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
  openapiv3AssetCount: number;
  openapiv3ToolCount: number;
  functionsAssetCount: number;
  functionsToolCount: number;
  externalMcpAssetCount: number;
  externalMcpToolCount: number;
};

function DeploymentActionsDropdown({
  deployment,
  latest,
}: {
  deployment: DeploymentSummary;
  latest: boolean;
}) {
  const [isOpen, setIsOpen] = useState(false);

  const redeployMutation = useRedeployDeployment({
    onSettled() {
      setIsOpen(false);
    },
  });

  // Find the current deployment to check its status
  const isCompletedDeployment = deployment.status === "completed";

  // Show actions for:
  // 1. Latest deployment (regardless of status) - shows "Retry"
  // 2. Completed deployments that are not the latest - shows "Redeploy"
  if (!latest && !isCompletedDeployment) {
    return null;
  }

  const handleRedeploy = () => {
    redeployMutation.mutate({
      request: {
        redeployRequestBody: {
          deploymentId: deployment.id,
        },
      },
    });
  };

  const isRedeploying = redeployMutation.isPending;
  const actionText = latest ? "Retry Deployment" : "Rollback";
  const buttonText = isRedeploying
    ? latest
      ? "Retrying Deployment..."
      : "Rolling Back..."
    : actionText;

  return (
    <DropdownMenu open={isOpen} onOpenChange={setIsOpen}>
      <DropdownMenuTrigger asChild>
        <Button variant="tertiary" size="sm" className="h-8 w-8 p-0">
          <Button.LeftIcon>
            <Icon name="ellipsis" className="size-4" />
          </Button.LeftIcon>
          <Button.Text className="sr-only">Open menu</Button.Text>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem
          onClick={handleRedeploy}
          disabled={redeployMutation.isPending}
          className="cursor-pointer"
        >
          <Icon name="refresh-cw" className="mr-2 size-4" />
          {buttonText}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

export function DeploymentsTable({
  showHeader = true,
}: { showHeader?: boolean } = {}) {
  const routes = useRoutes();
  const { data: res } = useListDeploymentsSuspense();
  const deployments = res.items ?? [];

  const { data: activeDeployment } = useActiveDeployment();

  if (deployments.length === 0) {
    return <DeploymentsEmptyState />;
  }

  const columnsWithData: TableProps<DeploymentSummary>["columns"] = [
    {
      key: "status",
      header: "",
      width: "50px",
      render: (row) => {
        switch (row.status) {
          case "completed":
            return (
              <div className="bg-success flex items-center justify-center rounded-full p-1">
                <Icon name="check" className="text-success-foreground" />
              </div>
            );
          case "failed":
            return (
              <div className="bg-destructive/20 flex items-center justify-center rounded-full p-1">
                <Icon name="x" className="text-destructive-foreground" />
              </div>
            );
          default:
            return (
              <div className="bg-warning flex items-center justify-center rounded-full p-1">
                <Icon
                  name="circle-dashed"
                  className="text-warning-foreground"
                />
              </div>
            );
        }
      },
    },
    {
      key: "id",
      header: "ID",
      render: (row) => {
        const createdAt = relativeTime(row.createdAt);

        return (
          <div>
            <DeploymentLink id={row.id} />
            <div className="flex gap-2">
              <p className="text-muted-foreground text-sm">{createdAt}</p>
              {activeDeployment === row && (
                <Badge variant="success" className="px-1.5 py-0.25">
                  Active
                </Badge>
              )}
            </div>
          </div>
        );
      },
    },
    {
      key: "assetCount",
      header: "Assets",
      render: (row) =>
        row.openapiv3AssetCount +
        row.functionsAssetCount +
        row.externalMcpAssetCount,
      width: "150px",
    },
    {
      key: "toolCount",
      header: "Tools",
      render: (row) =>
        row.openapiv3ToolCount +
        row.functionsToolCount +
        row.externalMcpToolCount,
      width: "0.5fr",
    },
    {
      key: "actions",
      header: "",
      render: (row) => {
        return (
          <DeploymentActionsDropdown
            deployment={row}
            latest={deployments[0] === row}
          />
        );
      },
      width: "auto",
    },
  ];

  return (
    <>
      {showHeader && (
        <>
          <div className="mb-2 flex items-center justify-between">
            <Heading variant="h2">Recent Deployments</Heading>
            {activeDeployment && (
              <routes.deployments.deployment.Link
                params={[activeDeployment.id]}
              >
                <Button variant="secondary" size="sm">
                  <Button.LeftIcon>
                    <Icon name="radio" className="size-4" />
                  </Button.LeftIcon>
                  <Button.Text>View Active Deployment</Button.Text>
                </Button>
              </routes.deployments.deployment.Link>
            )}
          </div>

          <div className="bg-secondary mb-6 space-y-2 rounded-lg p-6">
            <p className="text-muted-foreground text-sm">
              Each time you add a new source or update an existing source a new
              deployment is created.
            </p>
            <p className="text-muted-foreground text-sm">
              For each deployment all sources are analyzed in the project to
              generate or update the corresponding tool definitions.
            </p>
          </div>
        </>
      )}

      <Table<DeploymentSummary>
        columns={columnsWithData}
        rowKey={(row) => row.id}
        data={deployments}
        className="mb-8 overflow-auto"
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
