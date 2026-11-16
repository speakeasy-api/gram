import React from "react";

// Loaders for the setup pages. They sit next to route-pages.tsx so that file
// can export route helpers. The import() calls are what keep each page out of
// the initial route chunk.
export const SetupWizard = React.lazy(
  () => import("./pages/setup/SetupWizard"),
);
export const SetupTaskRedirect = React.lazy(
  () => import("./pages/setup/SetupTaskRedirect"),
);
