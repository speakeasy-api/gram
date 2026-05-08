import { Dialog } from "@/components/ui/dialog";
import { Type } from "@/components/ui/type";
import { useRoutes } from "@/routes";
import { Button } from "@speakeasy-api/moonshine";
import { AlertTriangle } from "lucide-react";

export function FatalErrorStep({
  error,
  onClose,
}: {
  error: string | null;
  onClose: () => void;
}) {
  const routes = useRoutes();

  return (
    <>
      <div className="flex flex-col items-center justify-center gap-4 py-8">
        <AlertTriangle className="text-destructive h-12 w-12" />
        <Type className="text-center text-lg font-medium">
          Configuration failed and cleanup didn't finish
        </Type>
        <Type muted small className="max-w-md text-center">
          {error ??
            "The OAuth proxy could not be created and the temporary environment could not be removed."}
        </Type>
        <Type muted small className="max-w-md text-center">
          Please review and remove the orphaned environment manually from the{" "}
          <routes.environments.Link>Environments page</routes.environments.Link>
          , then try again.
        </Type>
      </div>

      <Dialog.Footer className="flex justify-end">
        <Button onClick={onClose}>Close</Button>
      </Dialog.Footer>
    </>
  );
}
