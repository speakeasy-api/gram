import type * as THREE from "three";
import { create } from "zustand";

interface ASCIIStore {
  length: number;
  setLength: (length: number) => void;
  fontTexture: THREE.Texture | null;
  setFontTexture: (fontTexture: THREE.Texture) => void;
}

export const useAsciiStore = create<ASCIIStore>((set) => ({
  length: 0,
  setLength: (length) => set({ length }),
  fontTexture: null,
  setFontTexture: (fontTexture) => set({ fontTexture }),
}));
