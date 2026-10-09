import confetti from "canvas-confetti";
import { useCallback, useEffect, useRef } from "react";

// Brand language palette. canvas-confetti parses hex only, so the tokens are
// resolved from CSS at first use (they are hsl()), muted, and cached.
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

// Muted against the card so the pieces read as texture rather than a party
// popper: each brand hue is blended most of the way to the surface it lands on.
const MUTE = 0.55;

let confettiColorCache: string[] | null = null;

function rgbTriplet(el: HTMLElement): [number, number, number] {
  const rgb = getComputedStyle(el).color.match(/\d+/g);
  if (!rgb) return [136, 136, 136];
  return [Number(rgb[0]), Number(rgb[1]), Number(rgb[2])];
}

function brandConfettiColors(): string[] {
  if (confettiColorCache) return confettiColorCache;
  const probe = document.createElement("span");
  probe.style.display = "none";
  document.body.appendChild(probe);

  // The card surface, so muting follows the theme rather than a fixed grey.
  probe.style.color = "var(--bg-surface-primary-default)";
  const bg = rgbTriplet(probe);

  const colors = CONFETTI_TOKENS.map((token) => {
    probe.style.color = `var(--color-${token})`;
    const rgb = rgbTriplet(probe);
    return `#${rgb
      .map((n, i) => Math.round(n + (bg[i]! - n) * MUTE))
      .map((n) => n.toString(16).padStart(2, "0"))
      .join("")}`;
  });
  probe.remove();
  confettiColorCache = colors;
  return colors;
}

/**
 * A single burst on a local canvas, for a moment that happens to the reader
 * rather than one they hover into: the first hook event arriving on a setup
 * card, say. Keeps its own canvas and no timers — there is nothing to keep
 * going and nothing to stop.
 *
 * Defaults are tuned for a panel a few hundred pixels wide, firing upward
 * from the bottom edge. Pass overrides for anything else.
 */
export function useConfettiBurst(): {
  canvasRef: React.RefObject<HTMLCanvasElement | null>;
  burst: (overrides?: confetti.Options) => void;
} {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const fireRef = useRef<confetti.CreateTypes | null>(null);

  useEffect(() => {
    return () => {
      fireRef.current?.reset();
      fireRef.current = null;
    };
  }, []);

  const burst = useCallback((overrides?: confetti.Options) => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    fireRef.current ??= confetti.create(canvas, { resize: true });
    void fireRef.current({
      particleCount: 70,
      spread: 62,
      // Straight up from the bottom edge, so the pieces arc over the panel
      // and fall back through it.
      angle: 90,
      origin: { x: 0.5, y: 1 },
      startVelocity: 26,
      gravity: 0.9,
      decay: 0.9,
      scalar: 0.75,
      ticks: 190,
      colors: brandConfettiColors(),
      // The library honours the OS setting itself, so there is no separate
      // guard to keep in sync.
      disableForReducedMotion: true,
      ...overrides,
    });
  }, []);

  return { canvasRef, burst };
}
