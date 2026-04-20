import React from "react";

const MonacoEditorLazy = React.lazy(() =>
  import("@/components/monaco-editor").then((mod) => ({
    default: mod.MonacoEditor,
  })),
);

export default MonacoEditorLazy;
