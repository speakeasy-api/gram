import { ToolsetEntry } from "@gram/client/models/components";
import { createContext, useContext } from "react";
import { z } from "zod";

export const SuggestionSchema = z.object({
  name: z.string(),
  description: z.string(),
  inputs: z.array(
    z.object({
      name: z.string(),
      description: z.string(),
    }),
  ),
  steps: z.array(
    z.object({
      tool: z.string(),
      instructions: z.string(),
    }),
  ),
});

// Discriminated state: either the toolify dialog hasn't run (`toolset` is
// undefined and the other fields don't exist), or it has and every field is
// populated. Consumers narrow on `ctx.toolset` to access the rest.
export type ToolifyContextType =
  | {
      toolset?: undefined;
      purpose?: undefined;
      suggestion?: undefined;
    }
  | {
      toolset: ToolsetEntry;
      purpose: string;
      suggestion: z.infer<typeof SuggestionSchema>;
    };

export const emptyCtx: ToolifyContextType = {};

export const ToolifyContext = createContext<
  ToolifyContextType & { set: (t: ToolifyContextType) => void }
>({ ...emptyCtx, set: () => {} });

export const useToolifyContext = (): ToolifyContextType & {
  set: (t: ToolifyContextType) => void;
} => {
  return useContext(ToolifyContext);
};
