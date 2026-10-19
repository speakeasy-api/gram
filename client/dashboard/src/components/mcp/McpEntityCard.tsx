import { useIconDither } from "@/components/icon-dither";
import { Card } from "@/components/ui/Card";
import type { ComponentProps } from "react";

export function McpEntityCard(
  props: ComponentProps<typeof Card.Entity>,
): JSX.Element {
  const { canvasRef, start, stop } = useIconDither();
  return (
    <div className="h-full" onMouseEnter={start} onMouseLeave={stop}>
      <Card.Entity
        {...props}
        iconRailClassName="isolate"
        iconTileClassName="icon-hover-pulse"
        overlay={
          <>
            {props.overlay}
            <canvas
              ref={canvasRef}
              aria-hidden="true"
              className="pointer-events-none absolute inset-0 -z-10 size-full"
            />
          </>
        }
      />
    </div>
  );
}
