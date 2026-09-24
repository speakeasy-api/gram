import { CardContextMenu } from "@/components/card-context-menu";
import { useIconConfetti } from "@/components/icon-confetti";
import { Card } from "@/components/ui/Card";
import { MoreActions, type Action } from "@/components/ui/MoreActions";
import { Text } from "@/components/ui/Text";

import { cn } from "@/lib/utils";
import { Check, Code, FileCode } from "lucide-react";
import type { SourceOption } from "./source-list";
import { SourceFailureNotice } from "./source-list-notices";

/**
 * One source in the grid.
 *
 * Selection is optional: the browse page shows the same cards without it, so
 * a source reads the same wherever it is met. Actions are optional the same
 * way: the shelf offers them, the create flow's picker does not.
 */
export function SourceCard({
  source,
  selected,
  onSelect,
  onInspect,
  actions = [],
  failedDeploymentId,
}: {
  source: SourceOption;
  selected?: boolean;
  onSelect?: () => void;
  onInspect: () => void;
  /** The kebab menu and right-click menu, when the card offers them. */
  actions?: Action[];
  /** Set when this source caused the latest deployment to fail. */
  failedDeploymentId?: string | undefined;
}): JSX.Element {
  const { canvasRef, start, stop } = useIconConfetti();
  const Icon = source.kind === "openapi" ? FileCode : Code;
  const selectable = onSelect != null;
  const failing = failedDeploymentId !== undefined;
  return (
    <CardContextMenu actions={actions}>
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
          <div className="flex items-start justify-between gap-2">
            <div className="min-w-0 flex-1">
              <Text
                variant="subheading"
                as="div"
                className="text-md group-hover:text-primary truncate transition-colors"
                title={source.name}
              >
                {source.name}
              </Text>
              <Text small muted className="mt-1">
                {source.kind === "openapi" ? "OpenAPI document" : "Function"}
              </Text>
            </div>
            {/* The card itself is a button, so the controls in its corner
                stop clicks and keys before the card turns them into a
                select or inspect. */}
            {(failing || actions.length > 0) && (
              <div
                className="flex shrink-0 items-center gap-1"
                onClick={(e) => e.stopPropagation()}
                onKeyDown={(e) => e.stopPropagation()}
              >
                {failing && (
                  <SourceFailureNotice deploymentId={failedDeploymentId} />
                )}
                {actions.length > 0 && <MoreActions actions={actions} />}
              </div>
            )}
          </div>
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
                    <Check
                      className="text-background size-3.5"
                      strokeWidth={3}
                    />
                  </div>
                ) : (
                  <div className="border-border size-5 border" />
                )}
              </button>
            )}
          </div>
        </Card.Entity>
      </div>
    </CardContextMenu>
  );
}
