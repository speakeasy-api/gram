import { WorkerPoolContextProvider } from "@pierre/diffs/react";
import type { ReactNode } from "react";
import { workerFactory } from "./worker";

// Create a client component that wraps children with the worker pool.
// Import this in your layout to provide the worker pool to all pages.
export function HighlightProvider({
  children,
  langs = ["json", "yaml"],
}: {
  children: ReactNode;
  langs?: string[];
}): JSX.Element {
  return (
    <WorkerPoolContextProvider
      poolOptions={{ workerFactory }}
      highlighterOptions={{
        theme: { dark: "pierre-dark", light: "pierre-light" },
        preferredHighlighter: "shiki-wasm",
        langs,
      }}
    >
      {children}
    </WorkerPoolContextProvider>
  );
}
