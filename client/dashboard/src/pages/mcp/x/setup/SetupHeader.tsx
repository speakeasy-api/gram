import { BrandMeshLayers } from "@/components/brand-mesh";
import { useIconDither } from "@/components/icon-dither";
import { useEffect, type ReactNode } from "react";

/** The Setup page's header: the brand mesh, with a dither while it starts. */
export function SetupHeader({
  dither,
  children,
}: {
  dither: boolean;
  children: ReactNode;
}): JSX.Element {
  const { canvasRef, start, stop } = useIconDither();
  useEffect(() => {
    if (dither) start();
    return stop;
  }, [dither, start, stop]);
  return (
    <header className="relative isolate overflow-hidden bg-card">
      <div className="pointer-events-none absolute inset-0 -z-1 opacity-[0.367] saturate-[0.615]">
        <BrandMeshLayers />
      </div>
      {dither && (
        <canvas
          ref={canvasRef}
          aria-hidden="true"
          className="pointer-events-none absolute inset-0 -z-1 size-full opacity-20"
        />
      )}
      {children}
    </header>
  );
}
