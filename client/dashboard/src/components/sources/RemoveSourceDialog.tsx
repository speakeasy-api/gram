import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Input } from "@/components/ui/Input";
import { useSdkClient } from "@/contexts/Sdk";
import { useActiveDeployment } from "@/hooks/toolTypes";
import { slugify } from "@/lib/constants";
import { invalidateAllActiveDeployment } from "@gram/client/react-query/activeDeployment.js";
import { invalidateAllLatestDeployment } from "@gram/client/react-query/latestDeployment.js";
import { invalidateAllListDeployments } from "@gram/client/react-query/listDeployments.js";
import { invalidateAllListTools } from "@gram/client/react-query/listTools.js";
import { invalidateAllListToolsets } from "@gram/client/react-query/listToolsets.js";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2Icon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";

export type RemovableSource = {
  kind: "openapi" | "function";
  /** The deployment asset id, which is what the exclusion is keyed by. */
  assetId: string;
  name: string;
};

const KIND_LABEL: Record<RemovableSource["kind"], string> = {
  openapi: "OpenAPI source",
  function: "function source",
};

/**
 * Removes an OpenAPI document or function from the project by evolving the
 * active deployment without it. Its tools leave every server that carried
 * them, so the confirmation is typed.
 *
 * Shared by the sources list (row menu) and the source detail page (danger
 * zone), so the mutation lives here rather than with either caller.
 */
export function RemoveSourceDialog({
  source,
  open,
  onOpenChange,
  onRemoved,
}: {
  source: RemovableSource | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Called once the deployment has been evolved without the source. */
  onRemoved?: () => void;
}): JSX.Element {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        {source ? (
          <RemoveSourceDialogBody
            source={source}
            onClose={() => onOpenChange(false)}
            onRemoved={onRemoved}
          />
        ) : null}
      </Dialog.Content>
    </Dialog>
  );
}

function RemoveSourceDialogBody({
  source,
  onClose,
  onRemoved,
}: {
  source: RemovableSource;
  onClose: () => void;
  onRemoved?: () => void;
}): JSX.Element {
  const client = useSdkClient();
  const queryClient = useQueryClient();
  const { data: deploymentResult } = useActiveDeployment();
  const [pending, setPending] = useState(false);
  const [typed, setTyped] = useState("");

  const confirmation = slugify(source.name);
  const inputMatches = typed === confirmation;
  const label = KIND_LABEL[source.kind];
  const deploymentId = deploymentResult?.deployment?.id;

  const handleConfirm = async () => {
    if (!deploymentId) {
      toast.error("No active deployment to remove the source from.");
      return;
    }
    setPending(true);
    try {
      await client.deployments.evolveDeployment({
        evolveForm: {
          deploymentId,
          nonBlocking: true,
          ...(source.kind === "openapi"
            ? { excludeOpenapiv3Assets: [source.assetId] }
            : { excludeFunctions: [source.assetId] }),
        },
      });
      await Promise.all([
        invalidateAllActiveDeployment(queryClient),
        invalidateAllLatestDeployment(queryClient),
        invalidateAllListDeployments(queryClient),
        invalidateAllListTools(queryClient),
        // Toolsets carry the tool URNs the "Used in MCP" facet reads.
        invalidateAllListToolsets(queryClient),
      ]);
      toast.success(`Removed ${source.name}`);
      onClose();
      onRemoved?.();
    } catch (error) {
      console.error("Failed to remove source:", error);
      toast.error("Failed to remove the source. Please try again.");
    } finally {
      setPending(false);
    }
  };

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>Delete {label}</Dialog.Title>
        <Dialog.Description>
          This permanently removes {source.name} from the project. Tools
          generated from it are removed from every MCP server that includes
          them.
        </Dialog.Description>
      </Dialog.Header>
      <div className="grid gap-2">
        <span className="text-sm">
          To confirm, type "<strong>{confirmation}</strong>"
        </span>
        <Input value={typed} onChange={setTyped} disabled={pending} />
      </div>
      <Alert variant="warning" dismissible={false}>
        Deleting {confirmation} cannot be undone.
      </Alert>
      <Dialog.Footer>
        <Button variant="tertiary" onClick={onClose} disabled={pending}>
          <Button.Text>Cancel</Button.Text>
        </Button>
        <Button
          variant="destructive-primary"
          disabled={!inputMatches || pending}
          onClick={() => void handleConfirm()}
        >
          {pending ? (
            <Button.LeftIcon>
              <Loader2Icon className="size-4 animate-spin" />
            </Button.LeftIcon>
          ) : null}
          <Button.Text>{pending ? "Deleting" : "Delete"}</Button.Text>
        </Button>
      </Dialog.Footer>
    </>
  );
}
