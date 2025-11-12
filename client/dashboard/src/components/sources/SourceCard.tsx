import { CodeBlock } from "@/components/code";
// import { Dialog } from "@/components/ui/dialog";
import { Dialog } from "@speakeasy-api/moonshine";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/hover-card";
import { MoreActions } from "@/components/ui/more-actions";
import { SkeletonCode } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { UpdatedAt } from "@/components/updated-at";
import { useProject } from "@/contexts/Auth";
import { cn, getServerURL } from "@/lib/utils";
import { useRoutes } from "@/routes";
import { Asset } from "@gram/client/models/components";
import { useLatestDeployment } from "@gram/client/react-query/index.js";
import { HoverCardPortal } from "@radix-ui/react-hover-card";
import { CircleAlertIcon, FileCode, SquareFunction } from "lucide-react";
import { useEffect, useState } from "react";
import { useParams } from "react-router";

export type NamedAsset = Asset & {
  deploymentAssetId: string;
  name: string;
  slug: string;
  type: "openapi" | "function";
};

export function SourceCard({
  asset,
  causingFailure,
  handleRemove,
  handleAttachEnvironment,
  setChangeDocumentTargetSlug,
}: {
  asset: NamedAsset;
  causingFailure?: boolean | undefined;
  handleRemove: (assetId: string) => void;
  handleAttachEnvironment: (assetId: string) => void;
  setChangeDocumentTargetSlug: (slug: string) => void;
}) {
  const [documentViewOpen, setDocumentViewOpen] = useState(false);
  const IconComponent = asset.type === "openapi" ? FileCode : SquareFunction;

  const actions = [
    ...(asset.type === "openapi"
      ? [
          {
            label: "View",
            onClick: () => setDocumentViewOpen(true),
            icon: "eye" as const,
          },
          {
            label: "Update",
            onClick: () => setChangeDocumentTargetSlug(asset.slug),
            icon: "upload" as const,
          },
        ]
      : []),
    {
      label: "Attach Environment",
      onClick: () => {
        requestAnimationFrame(() => handleAttachEnvironment(asset.id));
      },
      icon: "globe" as const,
    },
    {
      label: "Delete",
      onClick: () => {
        requestAnimationFrame(() => handleRemove(asset.id));
      },
      icon: "trash" as const,
      destructive: true,
    },
  ];

  return (
    <div
      key={asset.id}
      className="bg-secondary max-w-sm text-card-foreground flex flex-col rounded-md border px-3 py-3"
    >
      <div className="flex items-center justify-between mb-2">
        <IconComponent className="size-5 shrink-0" strokeWidth={2} />
        <MoreActions actions={actions} />
      </div>

      <div
        onClick={
          asset.type === "openapi" ? () => setDocumentViewOpen(true) : undefined
        }
        className={cn(
          "leading-none mb-1.5",
          asset.type === "openapi" && "cursor-pointer",
        )}
      >
        <Type>{asset.name}</Type>
      </div>

      <div className="flex gap-1.5 items-center text-muted-foreground text-xs">
        {causingFailure && <AssetIsCausingFailureNotice />}
        <UpdatedAt date={asset.updatedAt} italic={false} className="text-xs" />
      </div>

      {asset.type === "openapi" && (
        <AssetViewDialog
          asset={asset}
          open={documentViewOpen}
          onOpenChange={setDocumentViewOpen}
        />
      )}
    </div>
  );
}

const AssetIsCausingFailureNotice = () => {
  const latestDeployment = useLatestDeployment();
  const routes = useRoutes();

  return (
    <HoverCard>
      <HoverCardTrigger
        className="cursor-pointer"
        aria-label="View deployment failure details"
      >
        <CircleAlertIcon className="size-3 text-destructive" />
      </HoverCardTrigger>
      <HoverCardPortal>
        <HoverCardContent side="bottom" className="text-sm" asChild>
          <div>
            <div>
              This API source caused the latest deployment to fail. Remove or
              update it to prevent future failures.
            </div>
            <div className="flex justify-end mt-3">
              <routes.deployments.deployment.Link
                className="text-link"
                params={[latestDeployment.data?.deployment?.id ?? ""]}
              >
                View Logs
              </routes.deployments.deployment.Link>
            </div>
          </div>
        </HoverCardContent>
      </HoverCardPortal>
    </HoverCard>
  );
};

function AssetViewDialog({
  asset,
  open,
  onOpenChange,
}: {
  asset: NamedAsset;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { projectSlug } = useParams();
  const project = useProject();
  const [content, setContent] = useState<string>("");
  const [isLoading, setIsLoading] = useState(false);

  const downloadURL = new URL("/rpc/assets.serveOpenAPIv3", getServerURL());
  downloadURL.searchParams.set("id", asset.id);
  downloadURL.searchParams.set("project_id", project.id);

  useEffect(() => {
    if (!open || !projectSlug) {
      setContent("");
      return;
    }

    fetch(downloadURL, {
      credentials: "same-origin",
    }).then((assetData) => {
      if (!assetData.ok) {
        setContent("");
        return;
      }
      setIsLoading(true);
      assetData.text().then((content) => {
        setContent(content);
        setIsLoading(false);
      });
    });
  }, [open, projectSlug]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="min-w-[80vw] h-[90vh]">
        <Dialog.Header>
          <Dialog.Title>{asset.name}</Dialog.Title>
        </Dialog.Header>
        <div className="flex-1 overflow-auto">
          {isLoading ? (
            <SkeletonCode lines={50} />
          ) : (
            <CodeBlock language="yaml" copyable>
              {content}
            </CodeBlock>
          )}
        </div>
      </Dialog.Content>
    </Dialog>
  );
}
