import { WorkerPoolContextProvider } from "@pierre/diffs/react";
import type { ReactNode } from "react";
import { workerFactory } from "./worker";

// Create a client component that wraps children with the worker pool.
// Import this in your layout to provide the worker pool to all pages.
export function HighlightProvider({ children }: { children: ReactNode }) {
  return (
    <WorkerPoolContextProvider
      poolOptions={{ workerFactory }}
      highlighterOptions={{
        theme: { dark: "pierre-dark", light: "pierre-light" },
        preferredHighlighter: "shiki-wasm",
        // Optionally preload languages to avoid lazy-loading delays
        langs: ["json", "yaml"],
      }}
    >
      {children}
    </WorkerPoolContextProvider>
  );
}
