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

// Every mounted card, so starting one can wipe the rest. Moving along a row
// would otherwise leave a trail of cards still finishing their fall behind the
// pointer — a pointer that has left is not a hover, and several at once reads
// as stuck animation rather than a response to where you are now.
const instances = new Set<{ clear: () => void }>();

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

  // Identity for the registry above, stable for this card's lifetime.
  const handleRef = useRef<{ clear: () => void } | null>(null);
  if (!handleRef.current) {
    handleRef.current = {
      clear: () => {
        if (fallTimerRef.current) {
          clearInterval(fallTimerRef.current);
          fallTimerRef.current = null;
        }
        // reset() cancels the animation but leaves the last frame painted, so
        // the pieces would freeze mid-air instead of going away.
        fireRef.current?.reset();
        const canvas = canvasRef.current;
        canvas?.getContext("2d")?.clearRect(0, 0, canvas.width, canvas.height);
      },
    };
  }

  useEffect(() => {
    const handle = handleRef.current;
    if (handle) instances.add(handle);
    return () => {
      if (fallTimerRef.current) clearInterval(fallTimerRef.current);
      fireRef.current?.reset();
      fireRef.current = null;
      if (handle) instances.delete(handle);
    };
  }, []);

  const stop = useCallback(() => {
    if (fallTimerRef.current) {
      clearInterval(fallTimerRef.current);
      fallTimerRef.current = null;
    }
    // The pieces already in the air finish their fall: this card is simply no
    // longer producing new ones.
  }, []);

  const start = useCallback(() => {
    const fire = getFire();
    if (!fire) return;

    for (const other of instances) {
      if (other !== handleRef.current) other.clear();
    }

    // The opening burst.
    void fire({
      particleCount: 34,
      spread: 360,
      // Tuned for a ~160px canvas: a slow launch with heavy drag keeps the
      // pieces inside the rail long enough to read, where the full-screen
      // defaults would shoot them off-canvas within a few frames.
      startVelocity: 7,
      gravity: 0.32,
      decay: 0.92,
      scalar: 0.5,
      ticks: 170,
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
        particleCount: 2,
        spread: 45,
        // Straight down, from a random point along the top edge.
        angle: 270,
        startVelocity: 3,
        gravity: 0.28,
        decay: 0.95,
        scalar: 0.45,
        ticks: 200,
        origin: { x: Math.random(), y: -0.1 },
        colors: brandConfettiColors(),
        disableForReducedMotion: true,
      });
    }, 320);
  }, [getFire]);

  return { canvasRef, start, stop };
}
