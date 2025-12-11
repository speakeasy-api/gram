import { memo, useEffect, useMemo, useRef } from "react";
import type { RefObject } from "react";
import { ASCIIEffect } from "./components/ascii-effect";
import { ScrollSyncPlane } from "./components/scroll-sync-plane";
import { CANVAS_PADDING } from "./constants";
import { useScrollUpdate } from "./hooks/use-scroll-update";
import { useWebGLStore } from "./store";
import { WebGLOut } from "./tunnel";
import { AsciiStars } from "./ascii-stars";
import { WindowTrail } from "./window-trail";
import { cn } from "@/lib/utils";
import { Canvas as R3FCanvas, useThree } from "@react-three/fiber";
import { EffectComposer } from "@react-three/postprocessing";
import * as THREE from "three";
import { useMoonshineConfig } from "@speakeasy-api/moonshine";
import { PerspectiveCamera } from "@react-three/drei";

const CanvasManager = ({
  containerRef,
}: {
  containerRef: RefObject<HTMLDivElement | null>;
}) => {
  const { theme: resolvedTheme } = useMoonshineConfig();
  const canvasZIndex = useWebGLStore((state) => state.canvasZIndex);
  const canvasBlendMode = useWebGLStore((state) => state.canvasBlendMode);
  const gl = useThree((state) => state.gl);
  const screenWidth = useThree((state) => state.size.width);
  const screenHeight = useThree((state) => state.size.height);
  const devicePixelRatio = useThree((state) => state.viewport.dpr);
  const setScreenWidth = useWebGLStore((state) => state.setScreenWidth);
  const setScreenHeight = useWebGLStore((state) => state.setScreenHeight);
  const setDpr = useWebGLStore((state) => state.setDpr);

  useEffect(() => {
    // Keep canvas transparent - only render where videos are
    gl.setClearColor(new THREE.Color(0, 0, 0));
    gl.setClearAlpha(0);
  }, [gl, resolvedTheme]);

  useEffect(() => {
    if (containerRef?.current) {
      containerRef.current.style.setProperty(
        "--canvas-z-index",
        canvasZIndex.toString(),
      );
      containerRef.current.style.setProperty("--blend-mode", canvasBlendMode);
    }
  }, [canvasZIndex, containerRef, canvasBlendMode]);

  useEffect(() => {
    setScreenWidth(screenWidth);
    setScreenHeight(screenHeight);
    setDpr(devicePixelRatio);
  }, [
    screenWidth,
    screenHeight,
    devicePixelRatio,
    setScreenWidth,
    setScreenHeight,
    setDpr,
  ]);

  return null;
};
CanvasManager.displayName = "CanvasManager";

const Scene = memo(() => {
  const scrollOffset = useWebGLStore((state) => state.scrollOffset);
  const elements = useWebGLStore((state) => state.elements);
  const showAsciiStars = useWebGLStore((state) => state.showAsciiStars);
  const size = useThree((state) => state.size);
  const resolutionRef = useRef(new THREE.Vector2(1, 1));

  const resolution = useMemo(() => {
    resolutionRef.current.set(size.width, size.height);
    return resolutionRef.current;
  }, [size.height, size.width]);

  return (
    <>
      {elements.map(({ element, fragmentShader, customUniforms }, index) => (
        <ScrollSyncPlane
          key={index}
          domElement={element}
          resolution={resolution}
          scrollOffset={scrollOffset}
          fragmentShader={fragmentShader}
          customUniforms={customUniforms}
        />
      ))}
      {showAsciiStars && (
        <>
          <AsciiStars count={150} opacity={0.6} />
          <WindowTrail />
        </>
      )}
    </>
  );
});
Scene.displayName = "Scene";

export const InnerCanvas = memo(
  ({ containerRef }: { containerRef: RefObject<HTMLDivElement | null> }) => {
    return (
      <>
        <R3FCanvas
          style={{ pointerEvents: "none" }}
          gl={{
            powerPreference: "high-performance",
            antialias: false,
            alpha: true,
            stencil: false,
            depth: false,
            outputColorSpace: THREE.SRGBColorSpace,
            toneMapping: THREE.NoToneMapping,
          }}
          onCreated={({ gl }) => {
            gl.setClearAlpha(0);
          }}
        >
          <Scene />

          <PerspectiveCamera makeDefault position={[0, 0, 10]} fov={20} />

          <CanvasManager containerRef={containerRef} />

          {/* ASCII Post Processing Effect */}
          <EffectComposer multisampling={0}>
            <ASCIIEffect />
          </EffectComposer>
          <WebGLOut />
        </R3FCanvas>
      </>
    );
  },
);
InnerCanvas.displayName = "InnerCanvas";

export const WebGLCanvas = () => {
  const containerRef = useRef<HTMLDivElement>(null);
  const canvasZIndex = useWebGLStore((state) => state.canvasZIndex);
  const isWebGLAvailable = useWebGLStore((state) => state.isWebGLAvailable);

  useScrollUpdate(containerRef);

  // Use full viewport height when visible (z-index >= 0), otherwise add padding for scroll
  const heightOffset = canvasZIndex >= 0 ? 1 : 1 + CANVAS_PADDING * 2;

  // Gracefully skip rendering if WebGL is not available
  if (!isWebGLAvailable) {
    return null;
  }

  return (
    <div
      ref={containerRef}
      style={
        {
          "--height-offset": heightOffset,
          zIndex: canvasZIndex,
        } as React.CSSProperties
      }
      className={cn(
        `pointer-events-none absolute top-0 left-0 overflow-hidden will-change-transform`,
        `h-[calc(100lvh*var(--height-offset))] w-full [mix-blend-mode:var(--blend-mode)]`,
      )}
    >
      <InnerCanvas containerRef={containerRef} />
    </div>
  );
};
