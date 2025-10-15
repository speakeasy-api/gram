import { useWebGLStore } from "../store";
import { cn } from "@/lib/utils";
import type { HTMLAttributes, Ref } from "react";
import { memo, useId } from "react";
import * as THREE from "three";
import { mergeRefs } from "react-merge-refs";

interface WebGLViewProps extends HTMLAttributes<HTMLDivElement> {
  fragmentShader: string;
  customUniforms?: Record<string, THREE.Uniform>;
  textureUrl?: string;
  ref?: Ref<HTMLDivElement>;
}

export const HtmlShadowElement = memo(
  ({
    fragmentShader,
    customUniforms,
    className,
    ref,
    ...props
  }: WebGLViewProps) => {
    const id = useId();
    const setElements = useWebGLStore((state) => state.setElements);

    const registerElement = (element: HTMLDivElement | null) => {
      if (!element) return;

      setElements((prevElements) => {
        const existingIndex = prevElements.findIndex(
          (e) => e.element === element,
        );

        // If element doesn't exist, add it
        if (existingIndex === -1) {
          const newElement = {
            element,
            fragmentShader,
            customUniforms: {
              ...customUniforms,
              u_time: new THREE.Uniform(0),
            },
          };
          return [...prevElements, newElement];
        }

        // Update existing element - create new array with updated element
        const updatedElement = {
          element,
          fragmentShader,
          customUniforms: {
            ...customUniforms,
            u_time: new THREE.Uniform(0),
          },
        };
        return [
          ...prevElements.slice(0, existingIndex),
          updatedElement,
          ...prevElements.slice(existingIndex + 1),
        ];
      });
    };

    return (
      <div
        key={id}
        id={`shadow-element-${id}`}
        ref={mergeRefs([registerElement, ref])}
        className={cn(
          "relative z-10 h-full w-full pointer-events-none",
          "[&_video]:opacity-0",
          className,
        )}
        {...props}
      />
    );
  },
);

HtmlShadowElement.displayName = "WebGLView";
