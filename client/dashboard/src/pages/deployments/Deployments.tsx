import { Page } from "@/components/page-layout";
import { Button } from "@speakeasy-api/moonshine";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Heading } from "@/components/ui/heading";
import { useRoutes } from "@/routes";
import {
  useListDeploymentsSuspense,
  useRedeployDeploymentMutation,
} from "@gram/client/react-query";
import { Icon, Table, TableProps } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Suspense, useState } from "react";
import { Outlet } from "react-router";
import { DeploymentsEmptyState } from "./DeploymentsEmptyState";

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

function DeploymentActionsDropdown({
  deploymentId,
  deployments,
}: {
  deploymentId: string;
  deployments: DeploymentSummary[];
}) {
  const queryClient = useQueryClient();
  const [isOpen, setIsOpen] = useState(false);

  const redeployMutation = useRedeployDeploymentMutation({
    onSuccess: () => {
      // Invalidate and refetch deployments list
      queryClient.invalidateQueries({
        queryKey: ["@gram/client", "deployments", "list"],
      });
      setIsOpen(false);
    },
    onError: (error) => {
      console.error("Failed to redeploy:", error);
      setIsOpen(false);
    },
  });

  // Check if this is the latest deployment (first item, assuming sorted by date desc)
  const isLatestDeployment =
    deployments.length > 0 && deployments[0]?.id === deploymentId;

  // Find the current deployment to check its status
  const currentDeployment = deployments.find((d) => d.id === deploymentId);
  const isCompletedDeployment = currentDeployment?.status === "completed";

  // Show actions for:
  // 1. Latest deployment (regardless of status) - shows "Retry"
  // 2. Completed deployments that are not the latest - shows "Redeploy"
  if (!isLatestDeployment && !isCompletedDeployment) {
    return null;
  }

  const handleRedeploy = () => {
    redeployMutation.mutate({
      request: {
        redeployRequestBody: {
          deploymentId,
        },
      },
    });
  };

  const isRedeploying = redeployMutation.isPending;
  const actionText = isLatestDeployment ? "Retry Deployment" : "Redeploy";
  const buttonText = isRedeploying ? "Redeploying..." : actionText;

  return (
    <DropdownMenu open={isOpen} onOpenChange={setIsOpen}>
      <DropdownMenuTrigger asChild>
        <Button variant="tertiary" size="sm" className="h-8 w-8 p-0">
          <Icon name="ellipsis" className="size-4" />
          <span className="sr-only">Open menu</span>
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

const columns: TableProps<DeploymentSummary>["columns"] = [
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
              <Icon name="circle-dashed" className="text-warning-foreground" />
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
          <p className="text-muted-foreground text-sm">{createdAt}</p>
        </div>
      );
    },
  },
  {
    key: "assetCount",
    header: "Assets",
    render: (row) => row.assetCount,
    width: "150px",
  },
  {
    key: "toolCount",
    header: "Tools",
    render: (row) => row.toolCount,
    width: "0.5fr",
  },
  {
    key: "actions",
    header: "",
    render: () => {
      // This will be overridden in DeploymentsTable with proper deployments prop
      return null;
    },
    width: "auto",
  },
];

function DeploymentsTable() {
  const { data: res } = useListDeploymentsSuspense();
  const deployments = res.items ?? [];

  if (deployments.length === 0) {
    return <DeploymentsEmptyState />;
  }

  const columnsWithData: TableProps<DeploymentSummary>["columns"] = [
    ...columns.slice(0, -1), // All columns except the last one (actions)
    {
      key: "actions",
      header: "",
      render: (row) => {
        return (
          <DeploymentActionsDropdown
            deploymentId={row.id}
            deployments={deployments}
          />
        );
      },
      width: "auto",
    },
  ];

  return (
    <>
      <Heading variant="h2">Recent Deployments</Heading>

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
