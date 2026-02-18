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
          <Icon name="refresh-cw" className="size-4 mr-2" />
          {buttonText}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function DeploymentsTable() {
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
              <div className="flex items-center justify-center rounded-full bg-success p-1">
                <Icon name="check" className="text-success-foreground" />
              </div>
            );
          case "failed":
            return (
              <div className="flex items-center justify-center rounded-full bg-destructive/20 p-1">
                <Icon name="x" className="text-destructive-foreground" />
              </div>
            );
          default:
            return (
              <div className="flex items-center justify-center rounded-full bg-warning p-1">
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
                <Badge variant="success" className="py-0.25 px-1.5">
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
      <Heading variant="h2">Recent Deployments</Heading>

      <div className="bg-secondary p-6 rounded-lg mb-6 space-y-2">
        <p className="text-sm text-muted-foreground">
          Each time you upload a new Gram Function or OpenAPI document or update
          a previously uploaded one, you create a new deployment in Gram. These
          are collectively called assets.
        </p>
        <p className="text-sm text-muted-foreground">
          For each deployment, Gram analyzes all related assets to generate or
          update the corresponding tool definitions.
        </p>
        <p className="text-sm text-muted-foreground">
          Gram also generates logs for every deployment, showing what was
          processed successfully and which functions or endpoints failed to
          convert into tools.
        </p>
      </div>

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
