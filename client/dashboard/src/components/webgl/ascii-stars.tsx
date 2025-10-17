import { useFrame } from "@react-three/fiber";
import { useMemo, useRef } from "react";
import * as THREE from "three";
import { useAsciiStore } from "./hooks/use-ascii-store";

interface AsciiStarsProps {
  count?: number;
  area?: [number, number]; // width, height in screen space
  speed?: number;
  opacity?: number;
  centerExclusionRadius?: number; // Radius around center to avoid
}

export function AsciiStars({
  count = 50,
  area = [30, 20],
  speed = 1,
  opacity = 0.3,
  centerExclusionRadius = 3,
}: AsciiStarsProps) {
  const meshRef = useRef<THREE.Points>(null);
  const fontTexture = useAsciiStore((state) => state.fontTexture);
  const asciiLength = useAsciiStore((state) => state.length);

  const { geometry, material } = useMemo(() => {
    const geometry = new THREE.BufferGeometry();
    const positions = new Float32Array(count * 3);
    const sizes = new Float32Array(count);
    const phases = new Float32Array(count);
    const speeds = new Float32Array(count);
    const charIndices = new Float32Array(count);
    const lifetimes = new Float32Array(count); // When star was "born"
    const durations = new Float32Array(count); // How long star lives

    for (let i = 0; i < count; i++) {
      // Random position - only on the right half of screen, avoid center
      let x, y, distFromRightCenter;
      const rightCenterX = area[0] * 0.25; // Center of right panel

      do {
        x = Math.random() * area[0] * 0.5; // Only positive X (right side)
        y = (Math.random() - 0.5) * area[1];
        // Calculate distance from center of right panel, not from [0,0]
        distFromRightCenter = Math.sqrt(Math.pow(x - rightCenterX, 2) + Math.pow(y, 2));
      } while (distFromRightCenter < centerExclusionRadius); // Keep trying if too close to center

      positions[i * 3] = x;
      positions[i * 3 + 1] = y;
      positions[i * 3 + 2] = Math.random() * -5 + 2; // depth between -3 and 2 (in front of camera at z=10)

      // Random size with more variation - smaller stars
      sizes[i] = Math.random() * Math.random() * 80 + 20; // Skewed towards smaller

      // Random phase for blinking
      phases[i] = Math.random() * Math.PI * 2;

      // Random blink speed
      speeds[i] = 0.5 + Math.random() * 1.5;

      // Just use first few characters for testing
      // Characters string is " -V-/V\\/A-•AV/\\•" (length should be 16)
      charIndices[i] = Math.floor(Math.random() * 3); // Use first 3 chars: space, -, V

      // Star lifecycle: random start time, lives for 5-10 seconds
      lifetimes[i] = Math.random() * 20; // Stagger initial spawns
      durations[i] = 5 + Math.random() * 5; // Live for 5-10 seconds
    }

    geometry.setAttribute("position", new THREE.BufferAttribute(positions, 3));
    geometry.setAttribute("size", new THREE.BufferAttribute(sizes, 1));
    geometry.setAttribute("phase", new THREE.BufferAttribute(phases, 1));
    geometry.setAttribute("speed", new THREE.BufferAttribute(speeds, 1));
    geometry.setAttribute("charIndex", new THREE.BufferAttribute(charIndices, 1));
    geometry.setAttribute("lifetime", new THREE.BufferAttribute(lifetimes, 1));
    geometry.setAttribute("duration", new THREE.BufferAttribute(durations, 1));

    const material = new THREE.ShaderMaterial({
      uniforms: {
        time: { value: 0 },
        fontTexture: { value: fontTexture },
        opacity: { value: opacity },
        asciiLength: { value: asciiLength },
      },
      vertexShader: `
        attribute float size;
        attribute float phase;
        attribute float speed;
        attribute float charIndex;
        attribute float lifetime;
        attribute float duration;

        varying float vAlpha;
        varying vec2 vUv;
        varying float vCharIndex;

        uniform float time;

        void main() {
          vUv = uv;
          vCharIndex = charIndex;

          // Calculate lifecycle: fade in, twinkle, fade out, respawn
          float age = mod(time - lifetime, duration);
          float fadeInTime = 1.0;
          float fadeOutTime = 1.0;
          float fadeIn = smoothstep(0.0, fadeInTime, age);
          float fadeOut = smoothstep(duration, duration - fadeOutTime, age);
          float lifecycle = fadeIn * fadeOut;

          // Calculate twinkling alpha based on phase and time
          float blink = sin(time * speed + phase);
          float twinkle = smoothstep(-0.8, 1.0, blink);

          // Combine lifecycle and twinkling
          vAlpha = lifecycle * twinkle;

          vec4 mvPosition = modelViewMatrix * vec4(position, 1.0);
          gl_PointSize = size;
          gl_Position = projectionMatrix * mvPosition;
        }
      `,
      fragmentShader: `
        uniform sampler2D fontTexture;
        uniform float opacity;
        uniform float asciiLength;

        varying float vAlpha;
        varying vec2 vUv;
        varying float vCharIndex;

        void main() {
          // Create varied shapes based on character index for variety
          vec2 coord = gl_PointCoord - vec2(0.5);
          float dist = length(coord);

          // Different patterns based on charIndex
          float pattern = 0.0;
          if (vCharIndex < 1.0) {
            // Small dot
            pattern = 1.0 - smoothstep(0.2, 0.3, dist);
          } else if (vCharIndex < 2.0) {
            // Plus shape
            float crossH = abs(coord.x) < 0.1 ? 1.0 : 0.0;
            float crossV = abs(coord.y) < 0.1 ? 1.0 : 0.0;
            pattern = max(crossH, crossV) * (1.0 - smoothstep(0.4, 0.5, dist));
          } else {
            // Star shape (asterisk)
            float angle = atan(coord.y, coord.x);
            float r = 0.3 + 0.1 * cos(4.0 * angle);
            pattern = 1.0 - smoothstep(r - 0.1, r, dist);
          }

          if (pattern < 0.1) discard;

          // Apply twinkling effect
          gl_FragColor = vec4(1.0, 1.0, 1.0, pattern * vAlpha);
        }
      `,
      transparent: true,
      depthWrite: false,
      blending: THREE.NormalBlending,
    });

    return { geometry, material };
  }, [fontTexture, count, area, opacity, asciiLength, centerExclusionRadius]);

  useFrame((state) => {
    if (meshRef.current && material) {
      material.uniforms.time.value = state.clock.elapsedTime;
    }
  });

  // Don't render until font texture is loaded
  if (!fontTexture) return null;

  return <points ref={meshRef} geometry={geometry} material={material} />;
}
