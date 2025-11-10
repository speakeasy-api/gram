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
import { useSession } from "@/contexts/Auth";
import { TriangleAlertIcon } from "lucide-react";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useRoutes } from "@/routes";

interface ApplyEnvironmentDialogContentProps {
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
  const session = useSession();
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
      <div className="flex gap-2 items-center">
        <Combobox
          value={activeEnvironmentId ?? ""}
          placeholder="select or create"
          options={(environments.data?.environments ?? []).map((env) => ({
            value: env.id,
            label: env.name,
          }))}
          onValueChange={setActiveEnvironmentId}
          createOptions={{
            renderCreatePrompt: (query) => (
              <div className="flex items-center gap-2">
                <Icon name="plus" /> Create "{query}"
              </div>
            ),
            handleCreate: (query) => {
              mutation.mutate({
                request: {
                  createEnvironmentForm: {
                    name: query,
                    description: `environment for attaching to source`,
                    entries: [],
                    organizationId: session.activeOrganizationId,
                  },
                },
              });
            },
          }}
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

      {selectedEnvironment && (
        <div className="space-y-2">
          <p className="text-sm text-muted-foreground">
            Variables in environment:
          </p>
          <div className="flex flex-wrap gap-2 items-center">
            {selectedEnvironment.entries.length > 0 ? (
              selectedEnvironment.entries.map((entry) => (
                <Badge key={entry.name}>{entry.name}</Badge>
              ))
            ) : (
              <div className="text-sm text-muted-foreground">Empty...</div>
            )}
            <routes.environments.environment.Link
              params={[selectedEnvironment.slug]}
            >
              <Button
                variant="tertiary"
                size="sm"
                aria-label="Edit environment"
              >
                <Icon name="eye" /> view
              </Button>
            </routes.environments.environment.Link>
          </div>
        </div>
      )}
    </div>
  );
}

function useSourceEnvironmentData(asset: NamedAsset) {
  const environments = useListEnvironments();
  const sourceEnvironment = useGetSourceEnvironment(
    {
      sourceKind: asset.type === "openapi" ? "http" : asset.type,
      sourceSlug: asset.slug,
    },
    undefined,
    {
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

export function ApplyEnvironmentDialogContent({
  asset,
  onClose,
}: ApplyEnvironmentDialogContentProps) {
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
    onSettled: () => {
      sourceEnvironment.refetch();
    },
  });

  const deleteSourceEnvironmentMutation =
    useDeleteSourceEnvironmentLinkMutation({
      onSettled: () => {
        sourceEnvironment.refetch();
      },
    });

  const handleConfirm = () => {
    if (!activeEnvironmentId && isDirty) {
      deleteSourceEnvironmentMutation.mutate({
        request: {
          sourceKind: asset.type === "openapi" ? "http" : asset.type,
          sourceSlug: asset.slug,
        },
      });
      return;
    }

    if (!activeEnvironmentId) return;

    setSourceEnvironmentMutation.mutate({
      request: {
        setSourceEnvironmentLinkRequestBody: {
          sourceKind: asset.type === "openapi" ? "http" : asset.type,
          sourceSlug: asset.slug,
          environmentId: activeEnvironmentId,
        },
      },
    });
  };

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>Apply Environment</Dialog.Title>
        <Dialog.Description>
          <p className="text-warning">
            <TriangleAlertIcon className="inline mr-2 w-4 h-4" />
            Environments attached here will apply to all users of tools from
            this source
          </p>
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
          Apply Environment
        </Button>
      </Dialog.Footer>
    </>
  );
}
