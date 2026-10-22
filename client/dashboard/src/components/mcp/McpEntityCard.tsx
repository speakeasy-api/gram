import { useIconDither } from "@/components/icon-dither";
import { Card } from "@/components/ui/Card";
import type { ComponentProps } from "react";

export function McpEntityCard({
  ditherPalette,
  ...props
}: ComponentProps<typeof Card.Entity> & {
  /** Colors for the hover sea; the theme's ink when omitted. */
  ditherPalette?: readonly string[];
}): JSX.Element {
  const { canvasRef, start, stop } = useIconDither({ palette: ditherPalette });
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
