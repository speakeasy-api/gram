import * as THREE from "three";
import { create } from "zustand";

interface WebGLElement {
  element: HTMLDivElement;
  fragmentShader: string;
  customUniforms?: Record<string, THREE.Uniform>;
}

interface WebGLStore {
  heroCanvasReady: boolean;
  elements: WebGLElement[];
  scrollOffset: THREE.Vector2;
  debug: boolean;
  canvasZIndex: number;
  canvasBlendMode: "lighten" | "darken" | "normal";
  screenWidth: number;
  screenHeight: number;
  dpr: number;
  showAsciiStars: boolean;
  setHeroCanvasReady: (ready: boolean) => void;
  setElements: (
    elements: WebGLElement[] | ((prev: WebGLElement[]) => WebGLElement[]),
  ) => void;
  setCanvasZIndex: (zIndex: number) => void;
  setCanvasBlendMode: (blendMode: "lighten" | "darken" | "normal") => void;
  setScreenWidth: (width: number) => void;
  setScreenHeight: (height: number) => void;
  setDpr: (dpr: number) => void;
  setShowAsciiStars: (show: boolean) => void;
}

export const useWebGLStore = create<WebGLStore>((set) => ({
  heroCanvasReady: false,
  elements: [],
  setElements: (elements) =>
    set((state) => ({
      elements:
        typeof elements === "function" ? elements(state.elements) : elements,
    })),
  scrollOffset: new THREE.Vector2(0, 0),
  debug: false,
  canvasZIndex: -1,
  canvasBlendMode: "normal",
  screenWidth: 0,
  screenHeight: 0,
  dpr: 1,
  showAsciiStars: false,
  setHeroCanvasReady: (ready) => set({ heroCanvasReady: ready }),
  setCanvasZIndex: (zIndex) => set({ canvasZIndex: zIndex }),
  setCanvasBlendMode: (blendMode) => set({ canvasBlendMode: blendMode }),
  setScreenWidth: (width) => set({ screenWidth: width }),
  setScreenHeight: (height) => set({ screenHeight: height }),
  setDpr: (dpr) => set({ dpr: dpr }),
  setShowAsciiStars: (show) => set({ showAsciiStars: show }),
}));
