import { useIconConfetti } from "@/components/icon-confetti";
import { Card } from "@/components/ui/Card";
import { Text } from "@/components/ui/Text";
import { useLatestDeployment } from "@/hooks/toolTypes";
import { cn } from "@/lib/utils";
import { Check, Code, FileCode } from "lucide-react";
import { useMemo } from "react";

/**
 * The grid of sources a project has, shared by the page that browses them and
 * the flow that builds a server from one.
 *
 * A "source" is an OpenAPI document or a function in the latest deployment —
 * the two kinds that still produce tools. Sources have no pages of their own
 * any more, so this is the whole of how they are shown.
 */
export type SourceOption = {
  key: string;
  name: string;
  kind: "openapi" | "function";
  /** Tools are matched to their source by these ids. */
  documentId?: string;
  functionId?: string;
};

/** The asset id a source is keyed by, which is what its panel refetches from. */
export function sourceAssetId(source: SourceOption): string {
  return source.documentId ?? source.functionId ?? "";
}

export function useProjectSources(): {
  sources: SourceOption[];
  isLoading: boolean;
} {
  const { data: deploymentResult, isLoading } = useLatestDeployment();
  const deployment = deploymentResult?.deployment;

  const sources = useMemo(() => {
    const openapi = (deployment?.openapiv3Assets ?? []).map((asset) => ({
      key: `openapi:${asset.id}`,
      name: asset.name,
      kind: "openapi" as const,
      documentId: asset.id,
    }));
    const functions = (deployment?.functionsAssets ?? []).map((asset) => ({
      key: `function:${asset.id}`,
      name: asset.name,
      kind: "function" as const,
      functionId: asset.id,
    }));
    return [...openapi, ...functions];
  }, [deployment]);

  return { sources, isLoading };
}

/**
 * One source in the grid.
 *
 * Selection is optional: the browse page shows the same cards without it, so
 * a source reads the same wherever it is met.
 */
export function SourceCard({
  source,
  selected,
  onSelect,
  onInspect,
}: {
  source: SourceOption;
  selected?: boolean;
  onSelect?: () => void;
  onInspect: () => void;
}): JSX.Element {
  const { canvasRef, start, stop } = useIconConfetti();
  const Icon = source.kind === "openapi" ? FileCode : Code;
  const selectable = onSelect != null;
  return (
    <div onMouseEnter={start} onMouseLeave={stop} className="h-full">
      <Card.Entity
        onClick={onSelect ?? onInspect}
        iconRailClassName="isolate"
        iconTileClassName="icon-hover-pulse"
        // Selection is the whole point of these cards where they can be
        // chosen, so it reads as a state on the card rather than a control
        // tucked inside it.
        className={cn(
          "cursor-pointer text-left",
          selected && "border-foreground ring-foreground ring-1",
        )}
        overlay={
          <canvas
            ref={canvasRef}
            aria-hidden="true"
            className="pointer-events-none absolute inset-0 -z-10 size-full"
          />
        }
        icon={<Icon className="text-foreground size-10" strokeWidth={1.25} />}
      >
        <Text
          variant="subheading"
          as="div"
          className="text-md group-hover:text-primary transition-colors"
        >
          {source.name}
        </Text>
        <Text small muted className="mt-1">
          {source.kind === "openapi" ? "OpenAPI document" : "Function"}
        </Text>
        <div className="mt-auto flex items-center justify-between gap-2 pt-3">
          {/* Named, not implicit: reading about a source is a different act
              from choosing it, and a bare card click hides that. */}
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              onInspect();
            }}
            // Card.Entity turns Enter/Space into its own onClick, so a
            // keyboard press here would select instead of opening the panel.
            onKeyDown={(e) => e.stopPropagation()}
            className="text-muted-foreground hover:text-foreground text-sm underline-offset-4 hover:underline"
          >
            Show details
          </button>
          {/* An explicit target for the choice: the ring alone reads as hover
              on a card that is already clickable everywhere. */}
          {selectable && (
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                onSelect();
              }}
              onKeyDown={(e) => e.stopPropagation()}
              aria-pressed={selected}
              className="hover:text-foreground flex items-center gap-2"
            >
              <Text small muted={!selected}>
                {selected ? "Selected" : "Select"}
              </Text>
              {selected ? (
                <div className="bg-foreground flex size-5 items-center justify-center">
                  <Check className="text-background size-3.5" strokeWidth={3} />
                </div>
              ) : (
                <div className="border-border size-5 border" />
              )}
            </button>
          )}
        </div>
      </Card.Entity>
    </div>
  );
}
