import { Dialog } from "@/components/ui/dialog";
import { Button } from "@speakeasy-api/moonshine";
import { ExternalLink, ShieldAlert } from "lucide-react";

interface PublicMcpWarningDialogProps {
  isOpen: boolean;
  onClose: () => void;
  onConfirm: () => void;
  isLoading?: boolean;
  environmentSlug: string;
  variableNames: string[];
}

export function PublicMcpWarningDialog({
  isOpen,
  onClose,
  onConfirm,
  isLoading = false,
  environmentSlug,
  variableNames,
}: PublicMcpWarningDialogProps) {
  const handleConfirm = () => {
    onConfirm();
    onClose();
  };

  return (
    <Dialog open={isOpen} onOpenChange={onClose}>
      <Dialog.Content
        className="max-w-md overflow-hidden p-0"
        style={{
          borderTop: "2px solid #C83228",
        }}
      >
        <div className="p-6">
          <Dialog.Header>
            <Dialog.Title className="flex items-center gap-2">
              <ShieldAlert
                className="h-5 w-5 shrink-0"
                style={{ color: "#C83228" }}
                aria-hidden="true"
              />
              Share system secrets with public callers.
            </Dialog.Title>
          </Dialog.Header>

          <div className="mt-4 space-y-4 text-sm">
            <Dialog.Description className="text-muted-foreground">
              Anyone with this URL will call with values from the Default
              Environment. System values are shared. Treat them as team or
              public credentials, not user credentials.
            </Dialog.Description>

            <div className="space-y-2">
              <p
                className="text-[11px] tracking-wider text-[#8B8684] uppercase"
                style={{ fontFamily: '"Diatype Mono", monospace' }}
              >
                Used by every public caller
              </p>
              <ul
                className="border-border bg-muted/30 max-h-40 space-y-1 overflow-y-auto rounded border p-3"
                style={{ fontFamily: '"Diatype Mono", monospace' }}
              >
                {variableNames.map((name) => (
                  <li key={name} className="text-sm font-light">
                    {name}
                  </li>
                ))}
              </ul>
            </div>

            <a
              href={`/environments/${environmentSlug}`}
              target="_blank"
              rel="noopener noreferrer"
              className="text-foreground inline-flex items-center gap-1 text-sm underline-offset-4 hover:underline"
            >
              Review in &quot;Default Environment&quot;
              <ExternalLink className="h-3.5 w-3.5" aria-hidden="true" />
            </a>
          </div>

          <Dialog.Footer className="mt-6 gap-2">
            <Button variant="tertiary" onClick={onClose}>
              Cancel
            </Button>
            <Button
              variant="destructive-primary"
              onClick={handleConfirm}
              disabled={isLoading}
            >
              {isLoading ? "Publishing..." : "Make public anyway."}
            </Button>
          </Dialog.Footer>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}
