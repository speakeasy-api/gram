import type dynamicIconImports from "lucide-react/dynamicIconImports";

// Type-only: importing the map at runtime makes Vite pre-bundle every icon as
// its own file in dev. See ./index.tsx.
export type IconName = keyof typeof dynamicIconImports;
