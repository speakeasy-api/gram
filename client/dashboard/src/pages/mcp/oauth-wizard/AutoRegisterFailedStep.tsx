import { Dialog } from "@/components/ui/dialog";
import { Link } from "@/components/ui/link";
import { Type } from "@/components/ui/type";
import { Button } from "@speakeasy-api/moonshine";
import { AlertTriangle } from "lucide-react";

export function AutoRegisterFailedStep({
  error,
  onClose,
}: {
  error: string | null;
  onClose: () => void;
}) {
  return (
    <>
      <div className="flex flex-col items-center justify-center gap-4 py-8">
        <AlertTriangle className="text-destructive h-12 w-12" />
        <Type className="text-center text-lg font-medium">
          Couldn't fetch credentials
        </Type>
        <Type muted small className="max-w-md text-center">
          {error ??
            "We weren't able to fetch OAuth credentials from the upstream provider."}
        </Type>
        <Type muted small className="max-w-md text-center">
          Please reach out to{" "}
          <Link external to="mailto:support@speakeasy.com">
            customer support
          </Link>{" "}
          and we'll help you get this MCP server connected.
        </Type>
      </div>

      <Dialog.Footer className="flex justify-end">
        <Button onClick={onClose}>Close</Button>
      </Dialog.Footer>
    </>
  );
}
