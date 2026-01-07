import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { Type } from "@/components/ui/type";
import { Toolset } from "@/lib/toolTypes";
import { useDraftToolset } from "@gram/client/react-query";
import { Button, Icon, Stack } from "@speakeasy-api/moonshine";
import { Minus, Plus } from "lucide-react";
import { useMemo, useState } from "react";

interface ReleaseChangesPopoverProps {
  toolset: Toolset;
  onRelease: () => void;
  isPending: boolean;
}

function extractToolName(urn: string, toolset: Toolset): string {
  const tool = toolset.tools.find((t) => t.toolUrn === urn);
  if (tool) {
    return tool.name;
  }
  const parts = urn.split(":");
  return parts[parts.length - 1] || urn;
}

function computeDiff(
  current: string[],
  draft: string[],
): { added: string[]; removed: string[] } {
  const currentSet = new Set(current);
  const draftSet = new Set(draft);

  const added = draft.filter((urn) => !currentSet.has(urn));
  const removed = current.filter((urn) => !draftSet.has(urn));

  return { added, removed };
}

export function ReleaseChangesPopover({
  toolset,
  onRelease,
  isPending,
}: ReleaseChangesPopoverProps) {
  const [isOpen, setIsOpen] = useState(false);

  const { data: draftToolset, isLoading } = useDraftToolset(
    { slug: toolset.slug },
    undefined,
    { enabled: isOpen && toolset.hasDraftChanges },
  );

  const toolChanges = useMemo(() => {
    if (!draftToolset?.draftToolUrns) {
      return { added: [], removed: [] };
    }
    return computeDiff(toolset.toolUrns, draftToolset.draftToolUrns);
  }, [toolset.toolUrns, draftToolset?.draftToolUrns]);

  const resourceChanges = useMemo(() => {
    if (!draftToolset?.draftResourceUrns) {
      return { added: [], removed: [] };
    }
    return computeDiff(toolset.resourceUrns, draftToolset.draftResourceUrns);
  }, [toolset.resourceUrns, draftToolset?.draftResourceUrns]);

  const totalAdded = toolChanges.added.length + resourceChanges.added.length;
  const totalRemoved =
    toolChanges.removed.length + resourceChanges.removed.length;

  const handleRelease = () => {
    onRelease();
    setIsOpen(false);
  };

  return (
    <Popover open={isOpen} onOpenChange={setIsOpen}>
      <PopoverTrigger asChild>
        <Button size="sm" disabled={isPending}>
          <Button.LeftIcon>
            <Icon name="rocket" className="h-4 w-4" />
          </Button.LeftIcon>
          <Button.Text>Release Changes</Button.Text>
          {(totalAdded > 0 || totalRemoved > 0) && (
            <span className="ml-1 text-xs opacity-80">
              ({totalAdded > 0 && `+${totalAdded}`}
              {totalAdded > 0 && totalRemoved > 0 && "/"}
              {totalRemoved > 0 && `-${totalRemoved}`})
            </span>
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent
        className="w-80"
        align="end"
        onClick={(e) => e.stopPropagation()}
      >
        <Stack gap={4}>
          <Stack gap={1}>
            <Type variant="body" className="font-semibold">
              {toolset.name}
            </Type>
            <Type variant="body" className="text-muted-foreground text-sm">
              Review changes before releasing to production
            </Type>
          </Stack>

          {isLoading ? (
            <Type variant="body" className="text-muted-foreground text-sm">
              Loading changes...
            </Type>
          ) : (
            <Stack gap={2}>
              <Type variant="body" className="text-sm font-medium">
                Changes Summary
              </Type>

              {/* Tool Changes */}
              {toolChanges.added.length > 0 && (
                <Stack gap={1}>
                  <div className="flex items-center gap-2 text-sm">
                    <Plus className="h-3 w-3 text-green-500" />
                    <span>
                      {toolChanges.added.length} tool
                      {toolChanges.added.length > 1 ? "s" : ""} added
                    </span>
                  </div>
                  <div className="space-y-0.5 pl-5 text-xs text-muted-foreground">
                    {toolChanges.added.slice(0, 5).map((urn) => (
                      <div key={urn} className="flex items-center gap-1">
                        <Plus className="h-2 w-2 text-green-500" />
                        <span className="truncate">
                          {extractToolName(urn, toolset)}
                        </span>
                      </div>
                    ))}
                    {toolChanges.added.length > 5 && (
                      <span className="text-muted-foreground">
                        ...and {toolChanges.added.length - 5} more
                      </span>
                    )}
                  </div>
                </Stack>
              )}

              {toolChanges.removed.length > 0 && (
                <Stack gap={1}>
                  <div className="flex items-center gap-2 text-sm">
                    <Minus className="h-3 w-3 text-red-500" />
                    <span>
                      {toolChanges.removed.length} tool
                      {toolChanges.removed.length > 1 ? "s" : ""} removed
                    </span>
                  </div>
                  <div className="space-y-0.5 pl-5 text-xs text-muted-foreground">
                    {toolChanges.removed.slice(0, 5).map((urn) => (
                      <div key={urn} className="flex items-center gap-1">
                        <Minus className="h-2 w-2 text-red-500" />
                        <span className="truncate">
                          {extractToolName(urn, toolset)}
                        </span>
                      </div>
                    ))}
                    {toolChanges.removed.length > 5 && (
                      <span className="text-muted-foreground">
                        ...and {toolChanges.removed.length - 5} more
                      </span>
                    )}
                  </div>
                </Stack>
              )}

              {/* Resource Changes */}
              {resourceChanges.added.length > 0 && (
                <div className="flex items-center gap-2 text-sm">
                  <Plus className="h-3 w-3 text-green-500" />
                  <span>
                    {resourceChanges.added.length} resource
                    {resourceChanges.added.length > 1 ? "s" : ""} added
                  </span>
                </div>
              )}

              {resourceChanges.removed.length > 0 && (
                <div className="flex items-center gap-2 text-sm">
                  <Minus className="h-3 w-3 text-red-500" />
                  <span>
                    {resourceChanges.removed.length} resource
                    {resourceChanges.removed.length > 1 ? "s" : ""} removed
                  </span>
                </div>
              )}

              {totalAdded === 0 && totalRemoved === 0 && (
                <Type variant="body" className="text-muted-foreground text-sm">
                  No tool or resource changes detected.
                </Type>
              )}
            </Stack>
          )}

          <Button onClick={handleRelease} disabled={isPending} className="w-full">
            {isPending ? "Releasing..." : "Release to Production"}
          </Button>
        </Stack>
      </PopoverContent>
    </Popover>
  );
}
