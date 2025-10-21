import { cn } from "@/lib/utils";
import { WebGLVideo } from "./components/webgl-video";

export interface AsciiVideoProps {
  videoSrc: string;
  className?: string;
  loop?: boolean;
  priority?: boolean;
  flipX?: boolean;
  flipY?: boolean;
}

/**
 * ASCII video component that renders video through the global ASCII shader.
 * NOTE: Requires WebGLCanvas and FontTexture to be rendered at the app root.
 */
export function AsciiVideo({
  videoSrc,
  className,
  loop = true,
  priority = false,
  flipX = false,
  flipY = false,
}: AsciiVideoProps) {
  return (
    <WebGLVideo
      textureUrl={videoSrc}
      loop={loop}
      priority={priority}
      flipX={flipX}
      flipY={flipY}
      className={cn("w-full h-full", className)}
    />
  );
}
