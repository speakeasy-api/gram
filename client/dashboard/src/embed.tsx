import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "@gram-ai/elements/elements.css";
import { ElementsEmbedStandalone } from "./pages/elements/ElementsEmbedStandalone";

/**
 * Minimal entry point for the Elements embed page.
 * Only loads elements CSS - completely isolated from dashboard styles.
 */
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ElementsEmbedStandalone />
  </StrictMode>,
);
