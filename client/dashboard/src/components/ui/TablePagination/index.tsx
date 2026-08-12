import { ChevronLeft, ChevronRight } from "lucide-react";

import { Button } from "@/components/ui/Button";
import { Text } from "@/components/ui/Text";

/**
 * "1–10 of 42" footer with previous/next arrows, for a `Table` paginated
 * client-side over rows already in memory. Renders nothing when everything
 * fits on one page.
 *
 * Owns no state: the caller holds the page index so it can reset to 0 when a
 * search, filter, or sort changes the row set under the current page.
 */
export function TablePagination({
  page,
  pageSize,
  totalItems,
  onPageChange,
}: {
  /** Zero-based index of the page currently rendered. */
  page: number;
  pageSize: number;
  /** Rows across every page, after filtering. */
  totalItems: number;
  onPageChange: (page: number) => void;
}): JSX.Element | null {
  const totalPages = Math.ceil(totalItems / pageSize);
  if (totalPages <= 1) return null;

  const firstShown = page * pageSize + 1;
  const lastShown = Math.min((page + 1) * pageSize, totalItems);

  return (
    <div className="flex items-center justify-between border-t px-4 py-3">
      <Text small muted>
        {firstShown}–{lastShown} of {totalItems}
      </Text>
      <div className="flex items-center gap-1">
        <Button
          variant="tertiary"
          size="sm"
          aria-label="Previous page"
          onClick={() => onPageChange(page - 1)}
          disabled={page === 0}
        >
          <ChevronLeft className="size-4" />
        </Button>
        <Button
          variant="tertiary"
          size="sm"
          aria-label="Next page"
          onClick={() => onPageChange(page + 1)}
          disabled={page >= totalPages - 1}
        >
          <ChevronRight className="size-4" />
        </Button>
      </div>
    </div>
  );
}
