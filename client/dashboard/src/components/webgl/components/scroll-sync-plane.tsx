import { Suspense, useLayoutEffect, useMemo, useRef } from "react";
import { glsl } from "@/lib/webgl/utils";
import { useFrame } from "@react-three/fiber";
import * as THREE from "three";

interface SharedUniforms {
  resolution: THREE.Vector2;
  scrollOffset: THREE.Vector2;
}

interface CustomShaderProps extends SharedUniforms {
  domElement: HTMLElement;
  fragmentShader: string;
  customUniforms?: Record<string, THREE.Uniform>;
  texture?: THREE.Texture | THREE.VideoTexture;
}

const commonVertex = glsl`
  precision mediump float;
  uniform vec2 uResolution;
  uniform vec2 uScrollOffset;
  uniform vec2 uDomXY;
  uniform vec2 uDomWH;

  varying vec2 v_uv;

  void main() {
    vec2 pixelXY = uDomXY - uScrollOffset + uDomWH * 0.5;
    pixelXY.y = uResolution.y - pixelXY.y;
    pixelXY += position.xy * uDomWH;
    vec2 xy = pixelXY / uResolution * 2.0 - 1.0;
    v_uv = uv;
    gl_Position = vec4(xy, 0.0, 1.0);
  }
`;

export const ScrollSyncPlane = ({
  domElement,
  resolution,
  scrollOffset,
  fragmentShader,
  customUniforms,
}: CustomShaderProps) => {
  const meshRef = useRef<THREE.Mesh>(null);
  const materialRef = useRef<THREE.ShaderMaterial>(null);

  useLayoutEffect(() => {
    const controller = new AbortController();
    const signal = controller.signal;

    const updateRect = () => {
      const rect = domElement.getBoundingClientRect();
      domXY.current.set(rect.left + window.scrollX, rect.top + window.scrollY);
      domWH.current.set(rect.width, rect.height);
    };
    updateRect();

    const resizeObserver = new ResizeObserver(updateRect);
    resizeObserver.observe(domElement);
    window.addEventListener("resize", updateRect, { signal });

    if (typeof window !== "undefined") {
      const bodyElement = document.body;
      resizeObserver.observe(bodyElement);
    }

    return () => {
      resizeObserver.disconnect();
      controller.abort();
    };
  }, [domElement]);

  useLayoutEffect(() => {
    if (!domElement) return;

    const observer = new window.IntersectionObserver(
      ([entry]) => {
        if (!meshRef.current) return;

        meshRef.current.visible = entry?.isIntersecting ?? false;
      },
      { threshold: 0 },
    );

    observer.observe(domElement);

    return () => observer.disconnect();
  }, [domElement]);

  const domWH = useRef<THREE.Vector2>(new THREE.Vector2(0, 0));
  const domXY = useRef<THREE.Vector2>(new THREE.Vector2(1, 1));
  const time = useRef(0);

  const uniforms = useMemo(
    () => ({
      uDomXY: { value: domXY.current },
      uDomWH: { value: domWH.current },
      uResolution: { value: resolution },
      uScrollOffset: { value: scrollOffset },
      uTime: { value: 0 },
      ...customUniforms,
    }),
    [resolution, scrollOffset, customUniforms],
  );

  useFrame(({ clock }) => {
    if (!meshRef.current || !materialRef.current) return;

    time.current = clock.getElapsedTime();
    materialRef.current.uniforms.uTime!.value = time.current;
    materialRef.current.uniformsNeedUpdate = true;
  });

  return (
    <Suspense>
      <mesh ref={meshRef}>
        <planeGeometry args={[1, 1, 1, 1]} />
        <shaderMaterial
          ref={materialRef}
          uniforms={uniforms}
          vertexShader={commonVertex}
          fragmentShader={fragmentShader}
          side={THREE.DoubleSide}
        />
      </mesh>
    </Suspense>
  );
};
