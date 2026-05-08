import { Dialog } from "@/components/ui/dialog";
import { Type } from "@/components/ui/type";
import { Button } from "@speakeasy-api/moonshine";
import { CheckCircle2 } from "lucide-react";

export function ResultStep({
  message,
  onClose,
}: {
  message: string;
  onClose: () => void;
}) {
  return (
    <>
      <div className="flex flex-col items-center justify-center gap-4 py-8">
        <CheckCircle2 className="h-12 w-12 text-emerald-500" />
        <Type className="text-center text-lg font-medium">
          OAuth Configured
        </Type>
        <Type muted small className="max-w-md text-center">
          {message}
        </Type>
      </div>

      <Dialog.Footer className="flex justify-end">
        <Button onClick={onClose}>Done</Button>
      </Dialog.Footer>
    </>
  );
}
