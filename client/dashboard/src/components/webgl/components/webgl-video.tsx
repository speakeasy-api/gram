import type { HTMLAttributes } from "react";
import { memo, Suspense, useEffect, useState } from "react";
import { HtmlShadowElement } from "./html-shadow-element";
import { WebGLIn } from "../tunnel";
import { useVideoTexture } from "@react-three/drei";
import * as THREE from "three";
import { glsl } from "@/lib/webgl/utils";

export interface VideoTexture extends THREE.VideoTexture {
  image: HTMLVideoElement;
}

const fragmentShader = glsl`
  precision mediump float;

  uniform vec2 u_resolution;
  uniform float u_time;
  varying vec2 v_uv;
  uniform sampler2D tDiffuse;
  uniform bool u_flipX;
  uniform bool u_flipY;

  void main() {
    vec2 uv = v_uv;
    if (u_flipX) {
      uv.x = 1.0 - uv.x;
    }
    if (u_flipY) {
      uv.y = 1.0 - uv.y;
    }
    vec4 color = texture2D(tDiffuse, uv);

    // if is full black, discard px
    if (color.rgb == vec3(0.0)) {
      discard;
    }

    gl_FragColor = color;
  }
`;

interface TextureLoaderProps {
  textureUrl: string;
  onTextureLoaded: (texture: VideoTexture) => void;
  options?: {
    loop?: boolean;
  };
}

const TextureLoader = memo(
  ({ textureUrl, onTextureLoaded, options }: TextureLoaderProps) => {
    const texture = useVideoTexture(textureUrl ?? "", options);

    useEffect(() => {
      if (texture) {
        onTextureLoaded(texture);
      }
    }, [texture, onTextureLoaded]);

    return null;
  },
);
TextureLoader.displayName = "TextureLoader";

interface WebGLVideoProps extends Omit<
  HTMLAttributes<HTMLDivElement>,
  "onMouseEnter" | "onMouseLeave"
> {
  textureUrl: string;
  flipX?: boolean;
  flipY?: boolean;
  hidden?: boolean;
  pause?: boolean;
  onMouseEnter?: (
    event: React.MouseEvent<HTMLDivElement> & { texture: VideoTexture },
  ) => void;
  onMouseLeave?: (
    event: React.MouseEvent<HTMLDivElement> & { texture: VideoTexture },
  ) => void;
  priority?: boolean;
  onLoad?: () => void;
  loop?: boolean;
  playbackRate?: number;
}

export const WebGLVideo = memo(
  ({
    textureUrl,
    flipX = false,
    flipY = false,
    hidden = false,
    loop = true,
    playbackRate = 1,
    onMouseEnter,
    onMouseLeave,
    priority: _priority = false,
    onLoad,
    ...props
  }: WebGLVideoProps) => {
    const [texture, setTexture] = useState<VideoTexture | null>(null);

    useEffect(() => {
      if (!texture) return;
      if (loop === undefined) return;

      if (!loop) {
        texture.image.loop = false;
        void texture.image.play();
      } else {
        texture.image.loop = true;
        void texture.image.play();
      }
    }, [loop, texture]);

    useEffect(() => {
      if (!texture) return;
      texture.image.playbackRate = playbackRate;
    }, [playbackRate, texture]);

    if (hidden) {
      return null;
    }

    return (
      <>
        <WebGLIn>
          <Suspense fallback={null}>
            <TextureLoader
              textureUrl={textureUrl}
              options={{
                loop,
              }}
              onTextureLoaded={(texture) => {
                setTexture(texture);
                if (onLoad) {
                  onLoad();
                }
              }}
            />
          </Suspense>
        </WebGLIn>
        {texture && (
          <HtmlShadowElement
            fragmentShader={fragmentShader}
            customUniforms={{
              tDiffuse: new THREE.Uniform(texture),
              u_flipX: new THREE.Uniform(flipX),
              u_flipY: new THREE.Uniform(flipY),
            }}
            onMouseEnter={(event) => {
              if (onMouseEnter) {
                onMouseEnter({
                  ...event,
                  texture,
                });
              }
            }}
            onMouseLeave={(event) => {
              if (onMouseLeave) {
                onMouseLeave({
                  ...event,
                  texture,
                });
              }
            }}
            {...props}
          />
        )}
      </>
    );
  },
);
WebGLVideo.displayName = "WebGLVideo";
