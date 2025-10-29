import { useFrame, useThree } from "@react-three/fiber";
import { memo, useEffect, useMemo, useRef } from "react";
import * as THREE from "three";
import { useWebGLStore } from "./store";

/**
 * Particle trail that follows draggable windows
 * Spawns particles at the window's screen position and converts to world coordinates
 */
export const WindowTrail = memo(function WindowTrail() {
  const meshRef = useRef<THREE.Points>(null);
  const draggableWindows = useWebGLStore((state) => state.draggableWindows);

  // Reusable vector for coordinate transformations (prevents 900 allocations/sec)
  const tempVector = useRef(new THREE.Vector3());

  // Create geometry and material once using useMemo
  const { geometry, material } = useMemo(() => {
    const geo = new THREE.BufferGeometry();
    const count = 600; // Increased pool size for longer trails

    const positions = new Float32Array(count * 3);
    const sizes = new Float32Array(count);
    const lifetimes = new Float32Array(count);
    const durations = new Float32Array(count);

    // Initialize all particles off-screen with expired lifetimes
    for (let i = 0; i < count; i++) {
      positions[i * 3] = 10000;
      positions[i * 3 + 1] = 10000;
      positions[i * 3 + 2] = -1000;
      sizes[i] = 50;
      lifetimes[i] = -10000;
      durations[i] = 2.0;
    }

    geo.setAttribute("position", new THREE.BufferAttribute(positions, 3));
    geo.setAttribute("size", new THREE.BufferAttribute(sizes, 1));
    geo.setAttribute("lifetime", new THREE.BufferAttribute(lifetimes, 1));
    geo.setAttribute("duration", new THREE.BufferAttribute(durations, 1));

    const mat = new THREE.ShaderMaterial({
      transparent: true,
      blending: THREE.AdditiveBlending,
      depthWrite: false,
      uniforms: {
        time: { value: 0 },
      },
      vertexShader: `
        attribute float size;
        attribute float lifetime;
        attribute float duration;

        varying float vAlpha;
        varying float vAge;

        uniform float time;

        void main() {
          // Calculate particle age
          float age = time - lifetime;

          // IMPORTANT: DO NOT CHANGE THESE FADE VALUES - THEY ARE CAREFULLY TUNED
          // Changing fadeInTime or fadeOutTime will break particle visibility
          // fadeInTime: 0.2s, fadeOutTime: 1.5s works with duration: 3.0-3.5s
          float fadeInTime = 0.2;
          float fadeOutTime = 1.5;
          float fadeIn = smoothstep(0.0, fadeInTime, age);
          float fadeOut = 1.0 - smoothstep(duration - fadeOutTime, duration, age);

          // Hide expired particles
          vAlpha = (age < 0.0 || age > duration) ? 0.0 : fadeIn * fadeOut;
          vAge = age / duration; // Normalized age for gradient

          vec4 mvPosition = modelViewMatrix * vec4(position, 1.0);
          gl_PointSize = size;
          gl_Position = projectionMatrix * mvPosition;
        }
      `,
      fragmentShader: `
        varying float vAlpha;
        varying float vAge;

        void main() {
          // Simple circular point
          float dist = length(gl_PointCoord - vec2(0.5));
          if (dist > 0.5) discard;

          // Simple white for now
          vec3 color = vec3(1.0, 1.0, 1.0);

          float alpha = vAlpha * (1.0 - smoothstep(0.3, 0.5, dist));
          gl_FragColor = vec4(color, alpha);
        }
      `,
    });

    return { geometry: geo, material: mat };
  }, []);

  const nextIdx = useRef(0);
  const prevTerminalPos = useRef({ x: -75, y: 75 });
  const prevEditorPos = useRef({ x: 75, y: -75 });
  const invalidate = useThree((state) => state.invalidate);

  // Cleanup geometry and material on unmount
  useEffect(() => {
    return () => {
      geometry.dispose();
      material.dispose();
    };
  }, [geometry, material]);

  useFrame((state) => {
    if (!meshRef.current || !geometry) {
      return;
    }

    const { camera, size } = state;
    const posAttr = geometry.attributes.position;
    const sizeAttr = geometry.attributes.size;
    const lifetimeAttr = geometry.attributes.lifetime;
    const durationAttr = geometry.attributes.duration;

    // Update shader time uniform
    material.uniforms.time.value = state.clock.elapsedTime;

    let needsUpdate = false;

    // Check terminal movement
    const terminalMoved =
      Math.abs(draggableWindows.terminal.x - prevTerminalPos.current.x) > 0.5 ||
      Math.abs(draggableWindows.terminal.y - prevTerminalPos.current.y) > 0.5;

    // Check editor movement
    const editorMoved =
      Math.abs(draggableWindows.editor.x - prevEditorPos.current.x) > 0.5 ||
      Math.abs(draggableWindows.editor.y - prevEditorPos.current.y) > 0.5;

    if (terminalMoved || editorMoved) {
      const windowPos = terminalMoved
        ? draggableWindows.terminal
        : draggableWindows.editor;

      // Only spawn particles if window is in the right half (x > -300)
      // This prevents particles from appearing on the left side form content
      if (windowPos.x < -300) {
        // Update previous positions even if not spawning
        prevTerminalPos.current = {
          x: draggableWindows.terminal.x,
          y: draggableWindows.terminal.y,
        };
        prevEditorPos.current = {
          x: draggableWindows.editor.x,
          y: draggableWindows.editor.y,
        };
        return;
      }

      // Calculate movement speed to adjust spawn rate
      const prevPos = terminalMoved
        ? prevTerminalPos.current
        : prevEditorPos.current;
      const distance = Math.sqrt(
        Math.pow(windowPos.x - prevPos.x, 2) +
          Math.pow(windowPos.y - prevPos.y, 2),
      );

      // Spawn more particles when moving fast (scale from 3 to 15 particles based on speed)
      const particleCount = Math.min(15, Math.max(3, Math.floor(distance / 2)));

      // Spawn multiple particles per frame for a denser trail
      for (let i = 0; i < particleCount; i++) {
        // Convert Framer Motion pixel offset to screen coordinates
        // Windows are centered in the RIGHT HALF of the screen (3/4 from left edge)
        // The RHS div is w-1/2 and starts at 50% of screen width
        const rhsCenterX = (size.width * 3) / 4; // Center of right half
        const screenX = rhsCenterX + windowPos.x + (Math.random() - 0.5) * 100;

        // Spawn from center of window, slightly behind
        const screenY =
          size.height / 2 + windowPos.y + (Math.random() - 0.5) * 60;

        // Convert screen pixels to NDC (Normalized Device Coordinates)
        const ndcX = (screenX / size.width) * 2 - 1;
        const ndcY = -(screenY / size.height) * 2 + 1;

        // Unproject to world coordinates at z=0 plane (reuse vector to avoid allocations)
        const vec = tempVector.current.set(ndcX, ndcY, 0.5);
        vec.unproject(camera as THREE.PerspectiveCamera);

        // Calculate ray from camera through unprojected point
        const dir = vec.sub(camera.position).normalize();

        // Find intersection with z=0 plane
        const distance = -camera.position.z / dir.z;
        const worldPos = camera.position
          .clone()
          .add(dir.multiplyScalar(distance));

        // Spawn particle slightly behind the window (negative z offset)
        const idx = nextIdx.current;
        posAttr.setXYZ(
          idx,
          worldPos.x,
          worldPos.y,
          worldPos.z - Math.random() * 0.5,
        );
        sizeAttr.setX(idx, 35 + Math.random() * 25);
        lifetimeAttr.setX(idx, state.clock.elapsedTime);
        durationAttr.setX(idx, 3.0 + Math.random() * 0.5); // Consistent duration for reliable trails

        nextIdx.current = (nextIdx.current + 1) % 600;
      }

      needsUpdate = true;
    }

    // Update previous positions
    prevTerminalPos.current = {
      x: draggableWindows.terminal.x,
      y: draggableWindows.terminal.y,
    };
    prevEditorPos.current = {
      x: draggableWindows.editor.x,
      y: draggableWindows.editor.y,
    };

    if (needsUpdate) {
      // Mark attributes for GPU upload
      posAttr.needsUpdate = true;
      sizeAttr.needsUpdate = true;
      lifetimeAttr.needsUpdate = true;
      durationAttr.needsUpdate = true;

      // Force geometry to recompute (fixes first-load particle issue)
      geometry.computeBoundingSphere();

      invalidate(); // Force R3F to re-render
    }
  });

  return <points ref={meshRef} geometry={geometry} material={material} />;
});
