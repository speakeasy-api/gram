import { Plugin } from "@/elements/types/plugins";
import chartPrompt from "@/elements/prompts/chart.txt?raw";
import { ChartRenderer } from "./component";

/**
 * This plugin renders charts using json-render format.
 */
export const chart: Plugin = {
  id: "chart",
  language: "chart",
  prompt: chartPrompt,
  Component: ChartRenderer,
  Header: undefined,
};
