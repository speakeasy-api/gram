import { useCallback, useEffect, useRef } from "react";

// CSS pixels per dither cell, and the dot drawn inside it. The gap keeps
// neighbouring dots from merging into heavy blocks.
const CELL = 3;
const DOT = 2;
// The field only advances on ticks, so the bands step like an old screen
// rather than glide.
const TICK_MS = 70;
// Ticks to fade the pattern fully in on hover, and fully out on leave.
const FADE_TICKS = 6;
// Ink opacity, so even the densest band is a grey wash rather than black.
const INK_ALPHA = 0.4;

// 4x4 ordered-dither thresholds. Comparing a smooth density against these is
// what turns a gradient into the checkerboard bands of a printed halftone.
const BAYER = [
  [0, 8, 2, 10],
  [12, 4, 14, 6],
  [3, 11, 1, 9],
  [15, 7, 13, 5],
].map((row) => row.map((v) => (v + 0.5) / 16));

// Smooth value noise in roughly [0, 1]: a hashed lattice, eased between
// corners. Cheap enough to sample a few thousand cells a tick.
function hash(x: number, y: number): number {
  const h = Math.sin(x * 127.1 + y * 311.7) * 43758.5453;
  return h - Math.floor(h);
}

function noise(x: number, y: number): number {
  const ix = Math.floor(x);
  const iy = Math.floor(y);
  const fx = x - ix;
  const fy = y - iy;
  const ux = fx * fx * (3 - 2 * fx);
  const uy = fy * fy * (3 - 2 * fy);
  const top = hash(ix, iy) + (hash(ix + 1, iy) - hash(ix, iy)) * ux;
  const bottom =
    hash(ix, iy + 1) + (hash(ix + 1, iy + 1) - hash(ix, iy + 1)) * ux;
  return top + (bottom - top) * uy;
}

// Three octaves of noise: broad shapes with finer detail riding on them.
function fbm(x: number, y: number): number {
  return (
    noise(x, y) * 0.57 + noise(x * 2, y * 2) * 0.29 + noise(x * 4, y * 4) * 0.14
  );
}

// Rolled fresh on every hover, so no two cards (or two visits) show the same
// sea: which way the swells run, and where in the noise they start.
type Sea = {
  angle: number;
  seedX: number;
  seedY: number;
  // The rail's centre, in cells: the swells turn about it, so a change of
  // heading sways the pattern rather than sweeping one far corner.
  cx: number;
  cy: number;
};

function rollSea(cols: number, rows: number): Sea {
  // Diagonal, never flat or vertical, travelling either way across the rail.
  const angle =
    (Math.random() < 0.5 ? 1 : -1) * (0.3 + Math.random() * 0.5) +
    (Math.random() < 0.5 ? 0 : Math.PI);
  return {
    angle,
    seedX: Math.random() * 100,
    seedY: Math.random() * 100,
    cx: cols / 2,
    cy: rows / 2,
  };
}

// How far, in radians, the heading wanders either side of where it started.
// Small and slow, like a swell easing round with the wind, not a dial turning.
const SWAY = 0.25;

// Wave density at a cell, peaking a little over 0.4 so the sea stays a light
// texture behind the icon. Swells roll along the heading; slow-drifting noise
// bends each front, and a second, broader strip of noise stretches and
// squeezes the spacing so some swells run long and others bunch up. Each swell
// shades up its flank and breaks into a thin empty foam line at the crest.
function density(col: number, row: number, t: number, sea: Sea): number {
  const warp = fbm(
    col * 0.03 + sea.seedX + t * 0.004,
    row * 0.03 + sea.seedY - t * 0.003,
  );
  const angle = sea.angle + (fbm(t * 0.004, sea.seedY) - 0.5) * 2 * SWAY;
  const x = col - sea.cx;
  const y = row - sea.cy;
  const along = x * Math.cos(angle) + y * Math.sin(angle);
  const across = y * Math.cos(angle) - x * Math.sin(angle);
  const stretch =
    1 +
    1.1 * (fbm(along * 0.025 + sea.seedX * 2, across * 0.02 + t * 0.002) - 0.5);
  const swell = Math.sin(along * 0.2 * stretch - t * 0.15 + warp * 2.5);
  if (swell > 0.93) return 0;
  const flank = Math.pow(0.5 + 0.5 * swell, 1.6);
  return 0.02 + flank * 0.42 + (warp - 0.5) * 0.08;
}

// Every mounted card, so starting one can wipe the rest. Moving along a row
// would otherwise leave a trail of cards still fading out behind the pointer —
// a pointer that has left is not a hover, and several at once reads as stuck
// animation rather than a response to where you are now.
const instances = new Set<{ clear: () => void }>();

/**
 * Hover effect on the card's icon rail: a black-and-white halftone sea, rolled
 * fresh on every hover. Drawn in the theme's foreground ink, so it is dark on
 * light and light on dark. Sits behind the icon tile: the rail is given `isolate` so
 * `-z-10` lands between the rail's own background and the tile, the same
 * layering the assistants card uses for its brand mesh.
 *
 * Fades in on enter and back out on leave rather than cutting, and stops its
 * frame loop once it has faded out.
 */
export function useIconDither(): {
  canvasRef: React.RefObject<HTMLCanvasElement | null>;
  start: () => void;
  stop: () => void;
} {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const hoveringRef = useRef(false);
  const rafRef = useRef<number | null>(null);
  const lastTickRef = useRef(0);
  const timeRef = useRef(0);
  const fadeRef = useRef(0);

  const halt = useCallback(() => {
    if (rafRef.current !== null) cancelAnimationFrame(rafRef.current);
    rafRef.current = null;
    fadeRef.current = 0;
    const canvas = canvasRef.current;
    canvas?.getContext("2d")?.clearRect(0, 0, canvas.width, canvas.height);
  }, []);

  // Identity for the registry above, stable for this card's lifetime.
  const handleRef = useRef<{ clear: () => void } | null>(null);
  if (!handleRef.current) {
    handleRef.current = {
      clear: () => {
        hoveringRef.current = false;
        halt();
      },
    };
  }

  useEffect(() => {
    const handle = handleRef.current;
    if (handle) instances.add(handle);
    return () => {
      halt();
      if (handle) instances.delete(handle);
    };
  }, [halt]);

  const stop = useCallback(() => {
    hoveringRef.current = false;
  }, []);

  const start = useCallback(() => {
    const canvas = canvasRef.current;
    const ctx = canvas?.getContext("2d");
    if (!canvas || !ctx) return;
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;

    for (const other of instances) {
      if (other !== handleRef.current) other.clear();
    }

    const dpr = window.devicePixelRatio || 1;
    canvas.width = Math.round(canvas.clientWidth * dpr);
    canvas.height = Math.round(canvas.clientHeight * dpr);
    const cols = Math.ceil(canvas.clientWidth / CELL);
    const rows = Math.ceil(canvas.clientHeight / CELL);
    if (cols === 0 || rows === 0) return;

    // Resolved on every hover rather than cached, so a theme switch is picked
    // up by the next one.
    canvas.style.color = "var(--foreground)";
    const ink = getComputedStyle(canvas).color;

    hoveringRef.current = true;
    if (rafRef.current !== null) return;
    const sea = rollSea(cols, rows);

    const loop = (now: number) => {
      rafRef.current = requestAnimationFrame(loop);
      if (now - lastTickRef.current < TICK_MS) return;
      lastTickRef.current = now;

      timeRef.current++;
      fadeRef.current = Math.min(
        FADE_TICKS,
        Math.max(0, fadeRef.current + (hoveringRef.current ? 1 : -1)),
      );
      if (fadeRef.current === 0 && !hoveringRef.current) {
        halt();
        return;
      }

      const level = fadeRef.current / FADE_TICKS;
      const t = timeRef.current;
      ctx.setTransform(1, 0, 0, 1, 0, 0);
      ctx.clearRect(0, 0, canvas.width, canvas.height);
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.fillStyle = ink;
      ctx.globalAlpha = INK_ALPHA;
      // One path for the whole frame: a few thousand cells as separate fills
      // would cost far more than the maths.
      ctx.beginPath();
      for (let row = 0; row < rows; row++) {
        const thresholds = BAYER[row % 4]!;
        for (let col = 0; col < cols; col++) {
          if (density(col, row, t, sea) * level > thresholds[col % 4]!) {
            ctx.rect(col * CELL, row * CELL, DOT, DOT);
          }
        }
      }
      ctx.fill();
    };
    rafRef.current = requestAnimationFrame(loop);
  }, [halt]);

  return { canvasRef, start, stop };
}
