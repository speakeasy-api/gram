import { Page } from "@/components/page-layout";
import { Heading } from "@/components/ui/heading";
import { useRoutes } from "@/routes";
import {
  useListDeploymentsSuspense,
  useRedeployDeploymentMutation,
} from "@gram/client/react-query";
import { Icon, Table, TableProps } from "@speakeasy-api/moonshine";
import { Suspense, useState } from "react";
import { Outlet } from "react-router";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useQueryClient } from "@tanstack/react-query";
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

  // Don't show actions for the latest deployment (first item, assuming sorted by date desc)
  const isLatestDeployment =
    deployments.length > 0 && deployments[0]?.id === deploymentId;

  // Find the current deployment to check its status
  const currentDeployment = deployments.find((d) => d.id === deploymentId);
  const isCompletedDeployment = currentDeployment?.status === "completed";

  // Only show actions for completed deployments that are not the latest
  if (isLatestDeployment || !isCompletedDeployment) {
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

  return (
    <DropdownMenu open={isOpen} onOpenChange={setIsOpen}>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" className="h-8 w-8 p-0">
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
          {redeployMutation.isPending ? "Redeploying..." : "Redeploy"}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

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
