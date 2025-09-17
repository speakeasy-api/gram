import { Page } from "@/components/page-layout";
import { Badge, Button } from "@speakeasy-api/moonshine";
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
import { toast } from "sonner";

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
};

const RedeploySuccessToast = ({ href }: { href: string | undefined }) => {
  if (!href) return <p>Successfully redeployed!</p>;

  return (
    <p>
      Successfully redeployed!{" "}
      <a href={href} className="underline">
        View deployment
      </a>
      .
    </p>
  );
};

function DeploymentActionsDropdown({
  deployment,
  latest,
}: {
  deployment: DeploymentSummary;
  latest: boolean;
}) {
  const queryClient = useQueryClient();
  const [isOpen, setIsOpen] = useState(false);
  const routes = useRoutes();

  const redeployMutation = useRedeployDeploymentMutation({
    onMutate: ({ request }) => {
      toast.loading("Redeploying...", {
        id: request.redeployRequestBody.deploymentId,
      });
    },
    onSuccess: ({ deployment }) => {
      // Invalidate and refetch deployments list
      queryClient.invalidateQueries({
        queryKey: ["@gram/client", "deployments", "list"],
      });
      const href = deployment?.id
        ? routes.deployments.deployment.href(deployment.id)
        : undefined;
      toast.success(() => <RedeploySuccessToast href={href} />);
    },
    onError: (error) => {
      console.error("Failed to redeploy:", error);
      toast.error(`Failed to redeploy. Please try again.`);
    },
    onSettled: (_, __, { request }) => {
      toast.dismiss(request.redeployRequestBody.deploymentId);
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
  const actionText = latest ? "Retry Deployment" : "Redeploy";
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

function DeploymentsTable() {
  const { data: res } = useListDeploymentsSuspense();
  const deployments = res.items ?? [];
  const activeDeployment = deployments.find((d) => d.status === "completed");

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
                <Badge size="xs" variant="success" className="py-0.25 px-1.5">
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
      render: (row) => row.openapiv3AssetCount,
      width: "150px",
    },
    {
      key: "toolCount",
      header: "Tools",
      render: (row) => row.openapiv3ToolCount,
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
