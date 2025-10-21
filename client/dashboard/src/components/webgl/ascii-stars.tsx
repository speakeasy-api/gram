import { useFrame, useThree } from "@react-three/fiber";
import { useEffect, useMemo, useRef, useState } from "react";
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
  speed: _speed = 1,
  opacity = 0.3,
  centerExclusionRadius = 3,
}: AsciiStarsProps) {
  const meshRef = useRef<THREE.Points>(null);
  const fontTexture = useAsciiStore((state) => state.fontTexture);
  const asciiLength = useAsciiStore((state) => state.length);

  const { geometry, material } = useMemo(() => {
    const geometry = new THREE.BufferGeometry();
    const maxClickStars = 50; // Reserve space for click-spawned stars
    const totalCount = count + maxClickStars;

    const positions = new Float32Array(totalCount * 3);
    const sizes = new Float32Array(totalCount);
    const phases = new Float32Array(totalCount);
    const speeds = new Float32Array(totalCount);
    const charIndices = new Float32Array(totalCount);
    const lifetimes = new Float32Array(totalCount); // When star was "born"
    const durations = new Float32Array(totalCount); // How long star lives

    for (let i = 0; i < count; i++) {
      // Random position - only on the right half of screen, avoid center
      let x, y, distFromRightCenter;
      const rightCenterX = area[0] * 0.25; // Center of right panel

      do {
        x = Math.random() * area[0] * 0.5; // Only positive X (right side)
        y = (Math.random() - 0.5) * area[1];
        // Calculate distance from center of right panel, not from [0,0]
        distFromRightCenter = Math.sqrt(
          Math.pow(x - rightCenterX, 2) + Math.pow(y, 2),
        );
      } while (distFromRightCenter < centerExclusionRadius); // Keep trying if too close to center

      positions[i * 3] = x;
      positions[i * 3 + 1] = y;
      positions[i * 3 + 2] = Math.random() * -5 + 2; // depth between -3 and 2 (in front of camera at z=10)

      // Better size distribution - mix of tiny, small, medium, and some large "hero" stars
      const sizeRoll = Math.random();
      if (sizeRoll < 0.6) {
        // 60% tiny stars
        sizes[i] = 15 + Math.random() * 25;
      } else if (sizeRoll < 0.9) {
        // 30% medium stars
        sizes[i] = 40 + Math.random() * 40;
      } else {
        // 10% large hero stars
        sizes[i] = 80 + Math.random() * 40;
      }

      // Random phase for blinking
      phases[i] = Math.random() * Math.PI * 2;

      // Random blink speed
      speeds[i] = 0.5 + Math.random() * 1.5;

      // Different star types: 0=small dot, 1=4-point star, 2=8-point star, 3=plus, 4=sparkle
      charIndices[i] = Math.floor(Math.random() * 5);

      // Star lifecycle: random start time, lives for 5-10 seconds
      lifetimes[i] = Math.random() * 20; // Stagger initial spawns
      durations[i] = 5 + Math.random() * 5; // Live for 5-10 seconds
    }

    // Initialize click stars area with invisible stars (far away)
    for (let i = count; i < totalCount; i++) {
      positions[i * 3] = 1000; // Far off screen
      positions[i * 3 + 1] = 1000;
      positions[i * 3 + 2] = -100;
      sizes[i] = 0;
      phases[i] = 0;
      speeds[i] = 1;
      charIndices[i] = 0;
      lifetimes[i] = -1000; // Already "expired"
      durations[i] = 1;
    }

    geometry.setAttribute("position", new THREE.BufferAttribute(positions, 3));
    geometry.setAttribute("size", new THREE.BufferAttribute(sizes, 1));
    geometry.setAttribute("phase", new THREE.BufferAttribute(phases, 1));
    geometry.setAttribute("speed", new THREE.BufferAttribute(speeds, 1));
    geometry.setAttribute(
      "charIndex",
      new THREE.BufferAttribute(charIndices, 1),
    );
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
          float angle = atan(coord.y, coord.x);

          // Different patterns based on charIndex
          float pattern = 0.0;

          if (vCharIndex < 1.0) {
            // Small soft dot
            pattern = 1.0 - smoothstep(0.15, 0.35, dist);
          }
          else if (vCharIndex < 2.0) {
            // 4-point star
            float r = 0.25 + 0.15 * cos(4.0 * angle);
            pattern = 1.0 - smoothstep(r - 0.05, r + 0.05, dist);
          }
          else if (vCharIndex < 3.0) {
            // 8-point star (more detailed)
            float r = 0.2 + 0.12 * cos(8.0 * angle);
            pattern = 1.0 - smoothstep(r - 0.03, r + 0.05, dist);
            // Add center glow
            pattern = max(pattern, (1.0 - smoothstep(0.0, 0.15, dist)) * 0.8);
          }
          else if (vCharIndex < 4.0) {
            // Plus/cross shape
            float crossH = smoothstep(0.12, 0.08, abs(coord.y));
            float crossV = smoothstep(0.12, 0.08, abs(coord.x));
            pattern = max(crossH, crossV) * (1.0 - smoothstep(0.35, 0.45, dist));
          }
          else {
            // Sparkle (4-point with rotation and glow)
            float sparkleAngle = angle + 0.785398; // 45 degree rotation
            float r = 0.15 + 0.2 * max(0.0, cos(4.0 * sparkleAngle));
            pattern = 1.0 - smoothstep(r - 0.02, r + 0.08, dist);
            // Bright center
            pattern = max(pattern, (1.0 - smoothstep(0.0, 0.1, dist)));
          }

          if (pattern < 0.05) discard;

          // Apply twinkling effect with slight brightness variation
          float brightness = 0.8 + vAlpha * 0.2; // Subtle brightness change
          gl_FragColor = vec4(brightness, brightness, brightness, pattern * vAlpha);
        }
      `,
      transparent: true,
      depthWrite: false,
      blending: THREE.NormalBlending,
    });

    return { geometry, material };
  }, [fontTexture, count, area, opacity, asciiLength, centerExclusionRadius]);

  const { camera, size } = useThree();
  const [clickStars, setClickStars] = useState<
    Array<{ x: number; y: number; z: number; time: number; id: number }>
  >([]);
  const nextIdRef = useRef(0);

  // Handle click to spawn stars - only when stars are visible
  useEffect(() => {
    // Only add listener when component is active (showAsciiStars is managed by Wizard)
    const handleClick = (event: MouseEvent) => {
      // Convert screen coordinates to normalized device coordinates
      const x = (event.clientX / size.width) * 2 - 1;
      const y = -(event.clientY / size.height) * 2 + 1;

      // Only spawn if click is on right half of screen
      if (x > 0) {
        // Convert to world space using camera's FOV and position
        // Camera is at z=10, fov=20 degrees
        const vFOV = (20 * Math.PI) / 180; // Convert to radians
        const height = 2 * Math.tan(vFOV / 2) * 10; // Height at z=0
        const width = height * (size.width / size.height);

        const worldX = x * (width / 2);
        const worldY = y * (height / 2);

        // Spawn a burst of 5-8 stars around click position
        const burstCount = 5 + Math.floor(Math.random() * 4);
        const newStars: Array<{
          x: number;
          y: number;
          z: number;
          time: number;
          id: number;
        }> = [];

        for (let i = 0; i < burstCount; i++) {
          const offset = 1.5; // Tighter cluster
          newStars.push({
            x: worldX + (Math.random() - 0.5) * offset,
            y: worldY + (Math.random() - 0.5) * offset,
            z: (Math.random() - 0.5) * 2, // Random depth
            time: Date.now(),
            id: nextIdRef.current++,
          });
        }

        setClickStars((prev) => [...prev, ...newStars]);

        // Remove these stars after their lifecycle (6 seconds)
        setTimeout(() => {
          setClickStars((prev) =>
            prev.filter((s) => !newStars.find((ns) => ns.id === s.id)),
          );
        }, 6000);
      }
    };

    // Only add listener when this component is rendered (which only happens when showAsciiStars is true)
    window.addEventListener("click", handleClick);
    return () => window.removeEventListener("click", handleClick);
  }, [camera, size]);

  // Update geometry with click stars
  useFrame((state) => {
    if (meshRef.current && material) {
      material.uniforms.time.value = state.clock.elapsedTime;

      // Update click stars
      if (geometry && clickStars.length > 0) {
        const posAttr = geometry.attributes.position;
        const sizeAttr = geometry.attributes.size;
        const lifetimeAttr = geometry.attributes.lifetime;
        const durationAttr = geometry.attributes.duration;
        const charIndexAttr = geometry.attributes.charIndex;

        clickStars.forEach((star, idx) => {
          const i = count + idx; // Append after base stars
          if (i >= posAttr.count) return; // Safety check

          posAttr.setXYZ(i, star.x, star.y, star.z);
          sizeAttr.setX(i, 60 + Math.random() * 60); // Medium to large
          lifetimeAttr.setX(i, star.time / 1000); // When it was spawned
          durationAttr.setX(i, 5); // Live for 5 seconds
          charIndexAttr.setX(i, 2 + Math.floor(Math.random() * 3)); // Use fancy shapes (8-point, plus, sparkle)
        });

        posAttr.needsUpdate = true;
        sizeAttr.needsUpdate = true;
        lifetimeAttr.needsUpdate = true;
        durationAttr.needsUpdate = true;
        charIndexAttr.needsUpdate = true;
      }
    }
  });

  // Don't render until font texture is loaded
  if (!fontTexture) return null;

  return <points ref={meshRef} geometry={geometry} material={material} />;
}
