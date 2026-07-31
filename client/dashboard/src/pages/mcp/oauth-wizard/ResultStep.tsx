import { Dialog } from "@/components/ui/Dialog";
import { Text } from "@/components/ui/Text";
import { Button } from "@/components/ui/Button";
import { CheckCircle2 } from "lucide-react";

export function ResultStep({
  message,
  onClose,
}: {
  message: string;
  onClose: () => void;
}): JSX.Element {
  return (
    <>
      <div className="flex flex-col items-center justify-center gap-4 py-8">
        <CheckCircle2 className="h-12 w-12 text-emerald-500" />
        <Text className="text-center text-lg font-medium">
          OAuth Configured
        </Text>
        <Text muted small className="max-w-md text-center">
          {message}
        </Text>
      </div>

      <Dialog.Footer className="flex justify-end">
        <Button onClick={onClose}>Done</Button>
      </Dialog.Footer>
    </>
  );
}
