import React from "react";

export default React.lazy(() =>
  import("@/components/monaco-editor").then((mod) => ({
    default: mod.MonacoEditor,
  })),
);
