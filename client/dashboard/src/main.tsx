import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import App from "./App.tsx";

// react-grab: dev-only overlay to copy any UI element's file/component/HTML
// context to the clipboard for pasting into a coding agent (⌘C / Ctrl+C on
// hover). `import.meta.env.DEV` is statically replaced with `false` in
// production builds, so this dynamic import is tree-shaken out entirely.
if (import.meta.env.DEV) {
  import("react-grab");
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <style>
      @import
      url('https://fonts.googleapis.com/css2?family=Inter:ital,opsz,wght@0,14..32,100..900;1,14..32,100..900&family=Mona+Sans:ital,wght@0,200..900;1,200..900&family=Roboto:ital,wght@0,100;0,300;0,400;0,500;0,700;0,900;1,100;1,300;1,400;1,500;1,700;1,900&family=Space+Mono:ital,wght@0,400;0,700;1,400;1,700&display=swap');
    </style>
    {/* Toaster is mounted inside App's provider tree (App.tsx) so it can read
        the active theme; a second instance here double-rendered every toast. */}
    <App />
  </StrictMode>,
);
