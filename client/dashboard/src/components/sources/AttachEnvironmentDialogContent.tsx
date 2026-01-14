import {
  Badge,
  Button,
  Combobox,
  Dialog,
  Icon,
} from "@speakeasy-api/moonshine";
// import { Dialog } from "@/components/ui/dialog";
import { NamedAsset } from "./SourceCard";
import {
  useCreateEnvironmentMutation,
  useListEnvironments,
  useGetSourceEnvironment,
  useSetSourceEnvironmentLinkMutation,
  useDeleteSourceEnvironmentLinkMutation,
} from "@gram/client/react-query";
import { useEffect, useState } from "react";
// import { useSession } from "@/contexts/Auth";
import { FileCode, FunctionSquare, TriangleAlertIcon } from "lucide-react";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useRoutes } from "@/routes";
import { toast } from "@/lib/toast";

interface AttachEnvironmentDialogContentProps {
  asset: NamedAsset;
  onClose: () => void;
}

interface EnvironmentComboboxProps {
  activeEnvironmentId: string | undefined;
  setActiveEnvironmentId: (id: string | undefined) => void;
}

function EnvironmentCombobox({
  activeEnvironmentId,
  setActiveEnvironmentId,
}: EnvironmentComboboxProps) {
  // const session = useSession();
  const routes = useRoutes();
  const environments = useListEnvironments();

  const mutation = useCreateEnvironmentMutation({
    onSuccess: (data) => {
      setActiveEnvironmentId(data.id);
    },
    onSettled: () => {
      environments.refetch();
    },
  });

  const selectedEnvironment = environments.data?.environments?.find(
    (env) => env.id === activeEnvironmentId,
  );

  return (
    <div className="flex flex-col gap-2">
      <div className="space-y-2">
        <div className="flex items-center justify-between">
          <p className="text-sm font-medium">Environment</p>
          {selectedEnvironment ? (
            <routes.environments.environment.Link
              params={[selectedEnvironment.slug]}
            >
              <Button
                variant="tertiary"
                size="sm"
                aria-label="View environment"
              >
                <Icon name="eye" /> view
              </Button>
            </routes.environments.environment.Link>
          ) : (
            <Button
              variant="tertiary"
              size="sm"
              aria-label="View environment"
              disabled
            >
              <Icon name="eye" /> view
            </Button>
          )}
        </div>
        <div className="flex gap-2 items-center w-full [&>:first-child]:min-w-64 [&>button]:justify-start">
          <Combobox
            value={activeEnvironmentId ?? ""}
            placeholder="select environment"
            options={(environments.data?.environments ?? []).map((env) => ({
              value: env.id,
              label: env.name,
            }))}
            onValueChange={setActiveEnvironmentId}
            /* add back after moonshine PR #319 ships */
            // createOptions={{
            //   renderCreatePrompt: (query) => (
            //     <div className="flex items-center gap-2">
            //       <Icon name="plus" /> Create "{query}"
            //     </div>
            //   ),
            //   handleCreate: (query) => {
            //     mutation.mutate({
            //       request: {
            //         createEnvironmentForm: {
            //           name: query,
            //           description: `environment for attaching to source`,
            //           entries: [],
            //           organizationId: session.activeOrganizationId,
            //         },
            //       },
            //     });
            //   },
            // }}
            loading={environments.isLoading || mutation.isPending}
          />
          {activeEnvironmentId && (
            <Button
              onClick={() => setActiveEnvironmentId(undefined)}
              variant="tertiary"
              size="sm"
              aria-label="Clear environment"
            >
              <Icon name="x" /> clear
            </Button>
          )}
        </div>
      </div>

      <div className="space-y-2 min-h-10">
        {selectedEnvironment && (
          <div className="flex flex-wrap gap-2 items-center">
            {selectedEnvironment.entries.length > 0 ? (
              selectedEnvironment.entries.map((entry) => (
                <Badge key={entry.name}>{entry.name}</Badge>
              ))
            ) : (
              <div className="text-sm text-muted-foreground">Empty...</div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function getSourceKind(
  assetType: NamedAsset["type"],
): "http" | "function" | null {
  if (assetType === "openapi") return "http";
  if (assetType === "function") return "function";
  return null; // externalmcp doesn't support environment attachment yet
}

function useSourceEnvironmentData(asset: NamedAsset) {
  const environments = useListEnvironments();
  const sourceKind = getSourceKind(asset.type);
  const sourceEnvironment = useGetSourceEnvironment(
    {
      sourceKind: (sourceKind ?? "http") as "http" | "function",
      sourceSlug: asset.slug,
    },
    undefined,
    {
      enabled: sourceKind !== null,
      retry: (_, err) => {
        if (err instanceof GramError && err.statusCode === 404) {
          return false;
        }
        return true;
      },
      throwOnError: false,
    },
  );

  return {
    environments,
    sourceEnvironment,
    isLoading: environments.isLoading || sourceEnvironment.isLoading,
  };
}

export function AttachEnvironmentDialogContent({
  asset,
  onClose,
}: AttachEnvironmentDialogContentProps) {
  const { sourceEnvironment } = useSourceEnvironmentData(asset);

  const [activeEnvironmentId, setActiveEnvironmentId] = useState<
    string | undefined
  >(undefined);

  const [initialEnvironmentId, setInitialEnvironmentId] = useState<
    string | undefined
  >(undefined);

  useEffect(() => {
    setActiveEnvironmentId(sourceEnvironment.data?.id);
    setInitialEnvironmentId(sourceEnvironment.data?.id);
  }, [sourceEnvironment.data?.id]);

  const isDirty = activeEnvironmentId !== initialEnvironmentId;

  const setSourceEnvironmentMutation = useSetSourceEnvironmentLinkMutation({
    onSuccess: () => {
      toast.success("Environment attached successfully", { persist: true });
      onClose();
    },
    onError: (error) => {
      toast.error("Failed to attach environment. Please try again.", { persist: true });
      console.error("Failed to attach environment:", error);
    },
    onSettled: () => {
      sourceEnvironment.refetch();
    },
  });

  const deleteSourceEnvironmentMutation =
    useDeleteSourceEnvironmentLinkMutation({
      onSuccess: () => {
        toast.success("Environment detached successfully", { persist: true });
        onClose();
      },
      onError: (error) => {
        toast.error("Failed to detach environment. Please try again.", { persist: true });
        console.error("Failed to detach environment:", error);
      },
      onSettled: () => {
        sourceEnvironment.refetch();
      },
    });

  const handleConfirm = () => {
    const sourceKind = getSourceKind(asset.type);
    if (!sourceKind) return; // externalmcp doesn't support environment attachment

    if (!activeEnvironmentId && isDirty) {
      deleteSourceEnvironmentMutation.mutate({
        request: {
          sourceKind,
          sourceSlug: asset.slug,
        },
      });
      return;
    }

    if (!activeEnvironmentId) return;

    setSourceEnvironmentMutation.mutate({
      request: {
        setSourceEnvironmentLinkRequestBody: {
          sourceKind,
          sourceSlug: asset.slug,
          environmentId: activeEnvironmentId,
        },
      },
    });
  };

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>Attach Environment</Dialog.Title>
        <Dialog.Description>
          <div className="space-y-2">
            <p className="text-warning">
              <TriangleAlertIcon className="inline mr-2 w-4 h-4" />
              Environments attached here will apply to all users of tools from
              this source in both public and private servers
            </p>
            {asset.type === "openapi" ? (
              <p className="flex items-center gap-1.5">
                Values set here will be forwarded to{" "}
                <span className="inline-flex items-center gap-1 bg-secondary px-1.5 py-0.5 rounded">
                  <FileCode className="w-3 h-3" /> {asset.name}
                </span>
              </p>
            ) : asset.type === "function" ? (
              <p className="flex items-center gap-1.5">
                You will be able to access values set here on{" "}
                <code className="text-xs bg-muted px-1 py-0.5 rounded">
                  process.env
                </code>{" "}
                in{" "}
                <span className="inline-flex items-center gap-1 bg-secondary px-1.5 py-0.5 rounded">
                  <FunctionSquare className="w-3 h-3" /> {asset.name}
                </span>
              </p>
            ) : null}
          </div>
        </Dialog.Description>
      </Dialog.Header>

      <EnvironmentCombobox
        activeEnvironmentId={activeEnvironmentId}
        setActiveEnvironmentId={setActiveEnvironmentId}
      />

      <Dialog.Footer>
        <Button onClick={onClose} variant="secondary">
          Cancel
        </Button>
        <Button
          onClick={handleConfirm}
          variant="primary"
          disabled={
            !isDirty ||
            setSourceEnvironmentMutation.isPending ||
            deleteSourceEnvironmentMutation.isPending
          }
        >
          Attach Environment
        </Button>
      </Dialog.Footer>
    </>
  );
}
