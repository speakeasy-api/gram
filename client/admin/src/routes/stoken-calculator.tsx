import { createFileRoute } from "@tanstack/react-router";

import { stokenSearchSchema } from "@/lib/stoken/url-state";
import { StokenCalculator } from "@/pages/stoken/StokenCalculator";

// The worksheet keeps its rows in the URL, the same way the organizations list
// keeps its filters: the address is the worksheet, so an operator can paste
// the estimate they are looking at to a colleague and get the same rows back.
export const Route = createFileRoute("/stoken-calculator")({
  component: StokenCalculator,
  validateSearch: stokenSearchSchema,
  staticData: { crumb: "S-token calculator" },
});
