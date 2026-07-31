import { Dialog } from "@/components/ui/Dialog";
import { Text } from "@/components/ui/Text";
import { useRoutes } from "@/routes";
import { Button } from "@/components/ui/Button";
import { AlertTriangle } from "lucide-react";

export function FatalErrorStep({
  error,
  onClose,
}: {
  error: string | null;
  onClose: () => void;
}): JSX.Element {
  const routes = useRoutes();

  return (
    <>
      <div className="flex flex-col items-center justify-center gap-4 py-8">
        <AlertTriangle className="text-destructive h-12 w-12" />
        <Text className="text-center text-lg font-medium">
          Configuration failed and cleanup didn't finish
        </Text>
        <Text muted small className="max-w-md text-center">
          {error ??
            "The OAuth proxy could not be created and the temporary environment could not be removed."}
        </Text>
        <Text muted small className="max-w-md text-center">
          Please review and remove the orphaned environment manually from the{" "}
          <routes.environments.Link>Environments page</routes.environments.Link>
          , then try again.
        </Text>
      </div>

      <Dialog.Footer className="flex justify-end">
        <Button onClick={onClose}>Close</Button>
      </Dialog.Footer>
    </>
  );
}
