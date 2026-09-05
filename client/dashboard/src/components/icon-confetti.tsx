import confetti from "canvas-confetti";
import { useCallback, useEffect, useRef } from "react";

// Brand language palette. canvas-confetti parses hex only, so the tokens are
// resolved from CSS at first use (they are hsl()) and cached.
const CONFETTI_TOKENS = [
  "brand-ruby",
  "brand-go",
  "brand-python",
  "brand-swift",
  "brand-java",
  "brand-terraform",
  "brand-unity",
  "brand-php",
  "brand-c",
];

let confettiColorCache: string[] | null = null;

function brandConfettiColors(): string[] {
  if (confettiColorCache) return confettiColorCache;
  const probe = document.createElement("span");
  probe.style.display = "none";
  document.body.appendChild(probe);
  const colors = CONFETTI_TOKENS.map((token) => {
    probe.style.color = `var(--color-${token})`;
    const rgb = getComputedStyle(probe).color.match(/\d+/g);
    if (!rgb) return "#888888";
    return `#${rgb
      .slice(0, 3)
      .map((n) => Number(n).toString(16).padStart(2, "0"))
      .join("")}`;
  });
  probe.remove();
  confettiColorCache = colors;
  return colors;
}

/**
 * Hover burst on the card's icon rail, fired through canvas-confetti so the
 * pieces get real physics — per-particle velocity, drift, gravity and tumble,
 * different on every fire. Sits behind the icon tile: the rail is given
 * `isolate` so `-z-10` lands between the rail's own background and the tile,
 * the same layering the assistants card uses for its brand mesh.
 *
 * The canvas is per-card and only ~160px wide, so the defaults (tuned for a
 * full-screen cannon) are scaled down: slower launch, smaller pieces, and a
 * short life so nothing lingers after the pointer leaves.
 */
export function useIconConfetti(): {
  canvasRef: React.RefObject<HTMLCanvasElement | null>;
  /** Burst once, then keep a light fall going until `stop`. */
  start: () => void;
  stop: () => void;
} {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const fireRef = useRef<confetti.CreateTypes | null>(null);
  const fallTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  // Built on first use rather than on mount: a grid of cards would otherwise
  // stand up a confetti instance apiece for an effect most of them never play.
  const getFire = useCallback((): confetti.CreateTypes | null => {
    if (fireRef.current) return fireRef.current;
    const canvas = canvasRef.current;
    if (!canvas) return null;
    fireRef.current = confetti.create(canvas, { resize: true });
    return fireRef.current;
  }, []);

  useEffect(() => {
    return () => {
      if (fallTimerRef.current) clearInterval(fallTimerRef.current);
      fireRef.current?.reset();
      fireRef.current = null;
    };
  }, []);

  const stop = useCallback(() => {
    if (fallTimerRef.current) {
      clearInterval(fallTimerRef.current);
      fallTimerRef.current = null;
    }
  }, []);

  const start = useCallback(() => {
    const fire = getFire();
    if (!fire) return;

    // The opening burst.
    void fire({
      particleCount: 110,
      spread: 360,
      // Tuned for a ~160px canvas: a slow launch with heavy drag keeps the
      // pieces inside the rail long enough to read, where the full-screen
      // defaults would shoot them off-canvas within a few frames.
      startVelocity: 11,
      gravity: 0.55,
      decay: 0.93,
      scalar: 0.6,
      ticks: 160,
      origin: { x: 0.5, y: 0.5 },
      colors: brandConfettiColors(),
      // The library honours the OS setting itself, so there is no separate
      // guard to keep in sync.
      disableForReducedMotion: true,
    });

    // Then a steady drift from the top edge for as long as the pointer stays,
    // so a long hover reads as falling confetti rather than one spent burst.
    // A few particles per tick keeps it sparse; the burst is the moment.
    if (fallTimerRef.current) return;
    fallTimerRef.current = setInterval(() => {
      void fire({
        particleCount: 3,
        spread: 55,
        // Straight down, from a random point along the top edge.
        angle: 270,
        startVelocity: 6,
        gravity: 0.5,
        decay: 0.94,
        scalar: 0.55,
        ticks: 130,
        origin: { x: Math.random(), y: -0.1 },
        colors: brandConfettiColors(),
        disableForReducedMotion: true,
      });
    }, 160);
  }, [getFire]);

  return { canvasRef, start, stop };
}
