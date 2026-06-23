import React from "react";

// Lazy so the heavy monaco-editor module (and its loader/worker side effects)
// only load when a CEL field actually mounts — not on every security-page
// render. Mirrors components/monaco-editor.lazy.tsx.
const CelMonacoEditorLazy = React.lazy(() =>
  import("./cel-monaco-editor").then((mod) => ({
    default: mod.CelMonacoEditor,
  })),
);

export default CelMonacoEditorLazy;
