import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useAsciiStore } from "../../hooks/use-ascii-store";
import { CanvasTexture } from "three";

const TEXTURE_SIZE = 1024;
const TEXTURE_STEPS = 256;
const FONT_SIZE = 64;

export function FontTexture() {
  const [container, setContainer] = useState<HTMLDivElement | null>(null);
  const canvasDebug = useRef<HTMLCanvasElement>(null);
  const canvas = useRef<HTMLCanvasElement>(null);

  const length = useAsciiStore((state) => state.length);
  const setFontTexture = useAsciiStore((state) => state.setFontTexture);

  const [characters] = useState(" -V-/V\\/A-•AV/\\•");

  const contextDebug = useMemo(() => {
    if (!container || !canvasDebug.current) return null;
    return canvasDebug.current.getContext("2d");
  }, [container, canvasDebug]);

  const context = useMemo(() => {
    if (!canvas.current || !container) return null;
    return canvas.current.getContext("2d");
  }, [canvas, container]);

  const texture = useMemo(() => {
    if (!canvas.current || !container) return null;
    return new CanvasTexture(canvas.current);
  }, [canvas, container]);

  useEffect(() => {
    if (!texture) return;
    setFontTexture(texture);
  }, [setFontTexture, texture]);

  const render = useCallback(() => {
    if (!context || !contextDebug || !texture) return;
    context.clearRect(0, 0, TEXTURE_SIZE, TEXTURE_SIZE);
    contextDebug.clearRect(0, 0, TEXTURE_SIZE, TEXTURE_SIZE);

    context.font = `${FONT_SIZE}px speakeasyAscii`;
    context.textAlign = "center";
    context.textBaseline = "middle";
    context.imageSmoothingEnabled = false;

    const charactersArray = characters.split("");
    const step = TEXTURE_STEPS / (length - 1);

    for (let i = 0; i < length; i++) {
      const x = i % 16;
      const y = Math.floor(i / 16);
      const c = step * i;
      contextDebug.fillStyle = `rgb(${c},${c},${c})`;
      contextDebug.fillRect(x * FONT_SIZE, y * FONT_SIZE, FONT_SIZE, FONT_SIZE);
    }

    charactersArray.forEach((character, i) => {
      const x = i % 16;
      const y = Math.floor(i / 16);

      context.fillStyle = "white";
      context.fillText(
        character,
        x * FONT_SIZE + FONT_SIZE / 2,
        y * FONT_SIZE + FONT_SIZE / 2,
      );
    });

    texture.needsUpdate = true;
  }, [characters, context, contextDebug, length, texture]);

  useEffect(() => {
    render();
  }, [render]);

  const [isHydrated, setIsHydrated] = useState(false);

  useEffect(() => {
    setIsHydrated(true);
  }, []);

  if (!isHydrated) return null;

  return (
    <div
      ref={(n) => setContainer(n)}
      className="fixed top-0 left-0 pointer-events-none hidden"
    >
      <canvas
        className="h-full w-full"
        width={TEXTURE_SIZE}
        height={TEXTURE_SIZE}
        ref={canvasDebug}
      />
      <canvas
        className="absolute inset-0 z-[1] h-full w-full"
        width={TEXTURE_SIZE}
        height={TEXTURE_SIZE}
        ref={canvas}
      />
    </div>
  );
}
