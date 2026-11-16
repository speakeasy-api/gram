import { lazy, StrictMode, Suspense } from "react";
import { createRoot } from "react-dom/client";
import App from "./App.tsx";

// Dev-only element context: hover and press ⌘C / Ctrl+C to copy.
if (import.meta.env.DEV) {
  void import("react-grab");
}

// Opt in per worktree with `mise run start:agentation`.
const Agentation =
  import.meta.env.DEV && import.meta.env.VITE_AGENTATION_ENABLED === "true"
    ? lazy(() =>
        import("agentation").then(({ Agentation }) => ({
          default: Agentation,
        })),
      )
    : null;

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <style>
      @import
      url('https://fonts.googleapis.com/css2?family=Inter:ital,opsz,wght@0,14..32,100..900;1,14..32,100..900&family=Mona+Sans:ital,wght@0,200..900;1,200..900&family=Roboto:ital,wght@0,100;0,300;0,400;0,500;0,700;0,900;1,100;1,300;1,400;1,500;1,700;1,900&family=Space+Mono:ital,wght@0,400;0,700;1,400;1,700&display=swap');
    </style>
    {/* Keep Toaster inside App for theme access; don’t mount it twice. */}
    <App />
    {Agentation && (
      <Suspense fallback={null}>
        <Agentation
          appName={`Speakeasy (${__GRAM_DEV_WORKTREE__})`}
          endpoint={
            import.meta.env.VITE_AGENTATION_URL ?? "http://localhost:4747"
          }
          className="top-[calc(50%-22px)] bottom-auto"
        />
      </Suspense>
    )}
  </StrictMode>,
);
