import { CodeBlock } from "@/components/code";
import { Page } from "@/components/page-layout";
import { MiniCards } from "@/components/ui/card-mini";
import { Dialog } from "@/components/ui/dialog";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@/components/ui/hover-card";
import { Input } from "@/components/ui/input";
import { MoreActions } from "@/components/ui/more-actions";
import { SkeletonCode } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { SimpleTooltip } from "@/components/ui/tooltip";
import { Type } from "@/components/ui/type";
import { UpdatedAt } from "@/components/updated-at";
import { FullWidthUpload } from "@/components/upload";
import { useProject } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { useTelemetry } from "@/contexts/Telemetry";
import { slugify } from "@/lib/constants";
import { cn, getServerURL } from "@/lib/utils";
import { useDeploymentLogsSummary } from "@/pages/deployments/deployment/Deployment";
import { useUploadOpenAPISteps } from "@/pages/onboarding/UploadOpenAPI";
import { UploadedDocument } from "@/pages/onboarding/Wizard";
import { useRoutes } from "@/routes";
import { Asset } from "@gram/client/models/components";
import {
  useLatestDeployment,
  useListAssets,
  useListTools,
} from "@gram/client/react-query/index.js";
import { HoverCardPortal } from "@radix-ui/react-hover-card";
import { Alert, Button, Icon } from "@speakeasy-api/moonshine";
import {
  CircleAlertIcon,
  FileCode,
  Loader2Icon,
  Plus,
  SquareFunction,
} from "lucide-react";
import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
} from "react";
import { useParams } from "react-router";
import { toast } from "sonner";
import AddSourceDialog, { AddSourceDialogRef } from "./AddSourceDialog";
import { SourcesEmptyState } from "./SourcesEmptyState";

type NamedAsset = Asset & {
  deploymentAssetId: string;
  name: string;
  slug: string;
  type: "openapi" | "function";
};

export function useDeploymentIsEmpty() {
  const { data: deploymentResult, isLoading } = useLatestDeployment();
  const deployment = deploymentResult?.deployment;

  if (isLoading) {
    return false;
  }

  return (
    !deployment ||
    (deployment.openapiv3Assets.length === 0 &&
      (deployment.functionsAssets?.length ?? 0) === 0 &&
      deployment.packages.length === 0)
  );
}

export default function Sources() {
  const client = useSdkClient();
  const routes = useRoutes();
  const telemetry = useTelemetry();
  const isFunctionsEnabled =
    telemetry.isFeatureEnabled("gram-functions") ?? false;

  const { data: deploymentResult, refetch, isLoading } = useLatestDeployment();
  const { data: assets, refetch: refetchAssets } = useListAssets();
  const deployment = deploymentResult?.deployment;

  const assetsCausingFailure = useUnusedAssetIds();

  const [isDeploying, setIsDeploying] = useState(false);
  const [changeDocumentTargetSlug, setChangeDocumentTargetSlug] = useState<
    string | null
  >(null);

  const addOpenAPIDialogRef = useRef<AddSourceDialogRef>(null);
  const removeSourceDialogRef = useRef<RemoveSourceDialogRef>(null);

  const finishUpload = () => {
    addOpenAPIDialogRef.current?.setOpen(false);
    setChangeDocumentTargetSlug(null);
    undoSpecUpload(); // Reset the file state
    refetch();
    refetchAssets();
  };

  const { handleSpecUpload, createDeployment, file, undoSpecUpload } =
    useUploadOpenAPISteps();

  const deploymentIsEmpty = useDeploymentIsEmpty();
  const deploymentLogsSummary = useDeploymentLogsSummary(deployment?.id);

  const logsCta = useMemo(() => {
    if (!deployment || !deploymentLogsSummary) {
      return null;
    }

    const hasErrors = deploymentLogsSummary.errors > 0;

    const icon = hasErrors ? (
      <Icon name="triangle-alert" className="text-yellow-500" />
    ) : (
      <Icon name="history" className="text-muted-foreground" />
    );

    const deploymentFailed = deployment.status === "failed";
    let tooltip = deploymentFailed
      ? "Latest deployment failed"
      : "Latest deployment succeeded";

    if (deploymentLogsSummary.skipped > 0) {
      tooltip += ` (${deploymentLogsSummary.skipped} operations skipped)`;
    }

    return (
      <Page.Section.CTA>
        <SimpleTooltip tooltip={tooltip}>
          <a href={routes.deployments.deployment.href(deployment.id)}>
            <Button
              variant="tertiary"
              className={cn(
                hasErrors &&
                  "text-yellow-600 dark:text-yellow-500 hover:bg-yellow-500/20!",
              )}
            >
              <Button.LeftIcon>{icon}</Button.LeftIcon>
              HISTORY
            </Button>
          </a>
        </SimpleTooltip>
      </Page.Section.CTA>
    );
  }, [deployment, deploymentLogsSummary]);

  const allSources: NamedAsset[] = useMemo(() => {
    if (!deployment || !assets) {
      return [];
    }

    const openApiSources = deployment.openapiv3Assets.map((deploymentAsset) => {
      const asset = assets.assets.find((a) => a.id === deploymentAsset.assetId);
      if (!asset) {
        throw new Error(`Asset ${deploymentAsset.assetId} not found`);
      }
      return {
        ...asset,
        deploymentAssetId: deploymentAsset.id,
        name: deploymentAsset.name,
        slug: deploymentAsset.slug,
        type: "openapi" as const,
      };
    });

    const functionSources = (deployment.functionsAssets ?? []).map(
      (deploymentAsset) => {
        const asset = assets.assets.find(
          (a) => a.id === deploymentAsset.assetId,
        );
        if (!asset) {
          throw new Error(`Asset ${deploymentAsset.assetId} not found`);
        }
        return {
          ...asset,
          deploymentAssetId: deploymentAsset.id,
          name: deploymentAsset.name,
          slug: deploymentAsset.slug,
          type: "function" as const,
        };
      },
    );

    return [...openApiSources, ...functionSources];
  }, [deployment, assets]);

  if (!isLoading && deploymentIsEmpty) {
    return (
      <>
        <SourcesEmptyState
          onNewUpload={() => addOpenAPIDialogRef.current?.setOpen(true)}
        />
        <AddSourceDialog ref={addOpenAPIDialogRef} />
      </>
    );
  }

  const removeSource = async (
    assetId: string,
    type: "openapi" | "function",
  ) => {
    try {
      await client.deployments.evolveDeployment({
        evolveForm: {
          deploymentId: deployment?.id,
          ...(type === "openapi"
            ? { excludeOpenapiv3Assets: [assetId] }
            : { excludeFunctions: [assetId] }),
        },
      });

      await Promise.all([refetch(), refetchAssets()]);

      toast.success(
        `${type === "openapi" ? "API" : "Function"} source deleted successfully`,
      );
    } catch (error) {
      console.error(`Failed to delete ${type} source:`, error);
      toast.error(
        `Failed to delete ${type === "openapi" ? "API" : "function"} source. Please try again.`,
      );
    }
  };

  const deploySpecUpdate = async (documentSlug: string) => {
    setIsDeploying(true);
    await createDeployment(documentSlug); // Make sure we overwrite the current document by slug
    finishUpload();
    toast.success("OpenAPI document deployed");
    setIsDeploying(false);
  };

  return (
    <>
      <Page.Section>
        <Page.Section.Title>Sources</Page.Section.Title>
        <Page.Section.Description>
          {isFunctionsEnabled
            ? "OpenAPI documents and Gram Functions providing tools for your toolsets"
            : "OpenAPI documents providing tools for your toolsets"}
        </Page.Section.Description>
        {logsCta}
        <Page.Section.CTA>
          <Button
            onClick={() => addOpenAPIDialogRef.current?.setOpen(true)}
            variant="secondary"
          >
            <Button.LeftIcon>
              <Plus className="w-4 h-4" />
            </Button.LeftIcon>
            <Button.Text>Add Source</Button.Text>
          </Button>
        </Page.Section.CTA>
        <Page.Section.Body>
          <MiniCards isLoading={isLoading}>
            {allSources?.map((asset: NamedAsset) => (
              <SourceCard
                key={asset.id}
                asset={asset}
                causingFailure={assetsCausingFailure.has(
                  asset.deploymentAssetId,
                )}
                onClickRemove={() => {
                  removeSourceDialogRef.current?.open(asset);
                }}
                setChangeDocumentTargetSlug={setChangeDocumentTargetSlug}
              />
            ))}
          </MiniCards>
          <Dialog
            open={changeDocumentTargetSlug !== null}
            onOpenChange={(open) => {
              if (!open) {
                setChangeDocumentTargetSlug(null);
                undoSpecUpload(); // Reset the file state when dialog closes
              }
            }}
          >
            <Dialog.Content className="max-w-2xl!">
              <Dialog.Header>
                <Dialog.Title>New OpenAPI Version</Dialog.Title>
                <Dialog.Description>
                  You are creating a new version of document{" "}
                  {changeDocumentTargetSlug}
                </Dialog.Description>
              </Dialog.Header>
              {!file ? (
                <FullWidthUpload
                  onUpload={handleSpecUpload}
                  allowedExtensions={["yaml", "yml", "json"]}
                />
              ) : (
                <UploadedDocument
                  file={file}
                  onReset={undoSpecUpload}
                  defaultExpanded
                />
              )}
              <Dialog.Footer>
                <Button
                  variant="tertiary"
                  onClick={() => {
                    setChangeDocumentTargetSlug(null);
                    undoSpecUpload(); // Reset the file state when dialog closes
                  }}
                >
                  Back
                </Button>
                <Button
                  onClick={() => deploySpecUpdate(changeDocumentTargetSlug!)}
                  disabled={!file || isDeploying || !changeDocumentTargetSlug}
                >
                  {isDeploying && <Spinner />}
                  {isDeploying ? "Deploying..." : "Deploy"}
                </Button>
              </Dialog.Footer>
            </Dialog.Content>
          </Dialog>
          <RemoveSourceDialog
            ref={removeSourceDialogRef}
            onConfirmRemoval={removeSource}
          />
          <AddSourceDialog ref={addOpenAPIDialogRef} />
        </Page.Section.Body>
      </Page.Section>
    </>
  );
}

interface RemoveSourceDialogRef {
  open: (asset: NamedAsset) => void;
  close: () => void;
}

interface RemoveSourceDialogProps {
  onConfirmRemoval: (
    assetId: string,
    type: "openapi" | "function",
  ) => Promise<void>;
}

const RemoveSourceDialog = forwardRef<
  RemoveSourceDialogRef,
  RemoveSourceDialogProps
>(({ onConfirmRemoval }, ref) => {
  const [open, setOpen] = useState(false);
  const [asset, setAsset] = useState<NamedAsset>({} as NamedAsset);
  const [pending, setPending] = useState(false);
  const [inputMatches, setInputMatches] = useState(false);

  const sourceSlug = slugify(asset.name);
  const sourceLabel =
    asset.type === "openapi" ? "API Source" : "Function Source";

  const resetState = () => {
    setAsset({} as NamedAsset);
    setInputMatches(false);
    setPending(false);
  };

  useImperativeHandle(ref, () => ({
    open: (assetToDelete: NamedAsset) => {
      setAsset(assetToDelete);
      setOpen(true);
      setInputMatches(false);
      setPending(false);
    },
    close: () => {
      resetState();
    },
  }));

  const handleOpenChange = (newOpen: boolean) => {
    setOpen(newOpen);
    if (!newOpen) {
      resetState();
    }
  };

  const handleConfirm = async () => {
    setPending(true);
    await onConfirmRemoval(asset.id, asset.type);
    setPending(false);

    setOpen(false);
    setInputMatches(false);
  };

  const DeleteButton = () => {
    if (pending) {
      return (
        <Button disabled variant="destructive-primary">
          <Button.LeftIcon>
            <Loader2Icon className="size-4 animate-spin" />
          </Button.LeftIcon>
          <Button.Text>Deleting {sourceLabel}</Button.Text>
        </Button>
      );
    }

    return (
      <Button
        disabled={!inputMatches}
        variant="destructive-primary"
        onClick={handleConfirm}
      >
        Delete {sourceLabel}
      </Button>
    );
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Delete {sourceLabel}</Dialog.Title>
          <Dialog.Description>
            This will permanently delete the{" "}
            {asset.type === "openapi" ? "API" : "gram function"} source and
            related resources such as tools within toolsets.
          </Dialog.Description>
        </Dialog.Header>
        <div className="grid gap-2">
          <span className="text-sm">
            To confirm, type "<strong>{sourceSlug}</strong>"
          </span>

          <Input onChange={(v) => setInputMatches(v === sourceSlug)} />
        </div>

        <Alert variant="error" dismissible={false}>
          Deleting {sourceSlug} cannot be undone.
        </Alert>

        <Dialog.Footer>
          <Button
            hidden={pending}
            onClick={() => handleOpenChange(false)}
            variant="tertiary"
          >
            Cancel
          </Button>
          <DeleteButton />
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
});

function SourceCard({
  asset,
  causingFailure,
  onClickRemove,
  setChangeDocumentTargetSlug,
}: {
  asset: NamedAsset;
  causingFailure?: boolean | undefined;
  onClickRemove: (assetId: string) => void;
  setChangeDocumentTargetSlug: (slug: string) => void;
}) {
  const [documentViewOpen, setDocumentViewOpen] = useState(false);
  const IconComponent = asset.type === "openapi" ? FileCode : SquareFunction;

  const actions =
    asset.type === "openapi"
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
          {
            label: "Delete",
            onClick: () => onClickRemove(asset.id),
            icon: "trash" as const,
            destructive: true,
          },
        ]
      : [
          {
            label: "Delete",
            onClick: () => onClickRemove(asset.id),
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
          <Dialog.Description>
            <UpdatedAt date={asset.updatedAt} italic={false} />
          </Dialog.Description>
        </Dialog.Header>
        {isLoading ? (
          <SkeletonCode />
        ) : (
          <CodeBlock className="overflow-auto">{content}</CodeBlock>
        )}
      </Dialog.Content>
    </Dialog>
  );
}

/**
 * Hook to identify asset IDs not referenced by any tools in the latest
 * deployment.
 */
const useUnusedAssetIds = () => {
  const latestDeployment = useLatestDeployment();
  const toolsList = useListTools(
    {
      deploymentId: latestDeployment.data?.deployment?.id ?? "",
    },
    undefined,
    {
      enabled: !!latestDeployment.data?.deployment?.id,
    },
  );

  const unusedAssetIds: Set<string> = useMemo(() => {
    const deployment = latestDeployment.data?.deployment;

    if (!toolsList.data || !deployment?.openapiv3Assets) {
      return new Set<string>();
    }

    // Build set of valid asset IDs (those referenced by tools)
    const validAssetIds = new Set(
      toolsList.data.tools
        .map((tool) => tool.httpToolDefinition?.openapiv3DocumentId)
        .filter((id): id is string => id != null),
    );

    // Find assets not referenced by any tool
    return new Set(
      deployment.openapiv3Assets
        .map((asset) => asset.id)
        .filter((id) => !validAssetIds.has(id)),
    );
  }, [toolsList.data, latestDeployment.data]);

  return unusedAssetIds;
};
