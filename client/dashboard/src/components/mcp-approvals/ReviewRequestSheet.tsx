import {
  ApprovalReview,
  RefreshEvidenceButton,
} from "@/components/mcp-approvals/ApprovalReview";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/Sheet";
import { useProject } from "@/contexts/Auth";

/**
 * The review surface for approval requests with no server page to live on
 * (stdio commands are not part of the URL inventory). URL targets review on
 * their Shadow MCP server page; this sheet keeps stdio reviews inside the
 * queue rather than on a page of their own.
 */
/** The slice of a review the sheet header needs; the body fetches the rest. */
export type ReviewSheetRequest = {
  id: string;
  targetRaw: string;
  requesterCount: number;
};

export function ReviewRequestSheet({
  request,
  open,
  onOpenChange,
}: {
  request: ReviewSheetRequest | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}): JSX.Element | null {
  const project = useProject();

  if (!request) return null;

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="sm:max-w-4xl">
        <SheetHeader>
          <SheetTitle className="break-all">{request.targetRaw}</SheetTitle>
          <SheetDescription>
            {`Requested by ${request.requesterCount} ${request.requesterCount === 1 ? "person" : "people"}.`}
          </SheetDescription>
        </SheetHeader>
        <div className="min-h-0 flex-1 space-y-4 overflow-y-auto px-4 pb-4">
          <div className="flex justify-end">
            <RefreshEvidenceButton
              requestId={request.id}
              projectSlug={project.slug}
              ready
            />
          </div>
          <ApprovalReview requestId={request.id} showDecide />
        </div>
      </SheetContent>
    </Sheet>
  );
}
