import { cn } from "@/lib/utils";
import { useEffect, useRef } from "react";

// Win95 "Starfield Simulation": points fly out of a vanishing point toward the
// viewer, each drawn as a short streak from where it was last frame. The
// vanishing point is pushed to the side the switch came FROM, so the whole
// field rakes across in the direction of travel.
const STAR_COUNT = 300;
const SPEED = 0.021;
const DEPTH = 1.9;
const VANISHING_POINT_OFFSET = 0.42;

type Star = { x: number; y: number; z: number };

function seedStar(): Star {
  return {
    // Square field in normalized space; the projection spreads it to the canvas.
    x: Math.random() * 2 - 1,
    y: Math.random() * 2 - 1,
    z: Math.random() * DEPTH + 0.1,
  };
}

/**
 * Decorative starfield behind the tab grid, drawn only while a mode switch is
 * in flight. `direction` is +1 when moving to a card on the right and -1 when
 * moving to one on the left.
 */
export function ModeSwitchStarfield({
  direction,
  fading = false,
  className,
}: {
  direction: 1 | -1;
  fading?: boolean;
  className?: string;
}): JSX.Element | null {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;

    const context = canvas.getContext("2d");
    if (!context) return;

    // The ink color follows the theme through the canvas element's own
    // computed color, so the field reads on both light and dark surfaces.
    const ink = getComputedStyle(canvas).color;
    const stars = Array.from({ length: STAR_COUNT }, seedStar);
    let frame = 0;

    const resize = () => {
      const ratio = Math.min(window.devicePixelRatio || 1, 2);
      canvas.width = canvas.clientWidth * ratio;
      canvas.height = canvas.clientHeight * ratio;
      context.setTransform(ratio, 0, 0, ratio, 0, 0);
    };
    resize();
    window.addEventListener("resize", resize);

    const draw = () => {
      const width = canvas.clientWidth;
      const height = canvas.clientHeight;
      // Origin sits opposite the travel direction: heading right means the
      // stars stream out of the left edge and rake rightwards.
      const originX = width * (0.5 - direction * VANISHING_POINT_OFFSET);
      const originY = height * 0.5;
      const spread = Math.max(width, height) * 0.75;

      context.clearRect(0, 0, width, height);
      context.strokeStyle = ink;
      context.lineCap = "round";

      for (const star of stars) {
        const previousZ = star.z;
        star.z -= SPEED;
        if (star.z <= 0.06) {
          Object.assign(star, seedStar(), { z: DEPTH });
          continue;
        }

        const x = originX + (star.x * spread) / star.z;
        const y = originY + (star.y * spread) / star.z;
        const previousX = originX + (star.x * spread) / previousZ;
        const previousY = originY + (star.y * spread) / previousZ;

        if (x < -80 || x > width + 80 || y < -80 || y > height + 80) continue;

        // Nearer stars are brighter and thicker — the depth cue that makes the
        // field read as motion rather than noise.
        const nearness = 1 - star.z / DEPTH;
        context.globalAlpha = 0.18 + nearness * 0.55;
        context.lineWidth = 0.7 + nearness * 2.2;
        context.beginPath();
        context.moveTo(previousX, previousY);
        context.lineTo(x, y);
        context.stroke();
      }

      context.globalAlpha = 1;
      frame = window.requestAnimationFrame(draw);
    };
    frame = window.requestAnimationFrame(draw);

    return () => {
      window.cancelAnimationFrame(frame);
      window.removeEventListener("resize", resize);
    };
  }, [direction]);

  return (
    <canvas
      ref={canvasRef}
      aria-hidden="true"
      className={cn(
        "text-default-fixed-light pointer-events-none fixed inset-0 z-0 h-full w-full transition-opacity duration-500",
        fading ? "opacity-0" : "opacity-100",
        className,
      )}
    />
  );
}
