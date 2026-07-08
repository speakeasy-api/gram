import { PromptTemplateKind } from "@gram/client/models/components/prompttemplate.js";
import { useTemplates } from "@gram/client/react-query/templates.js";

export function useCustomTools(): {
  customTools:
    | NonNullable<ReturnType<typeof useTemplates>["data"]>["templates"]
    | undefined;
  isLoading: boolean;
} {
  const { data, isLoading } = useTemplates();
  return {
    customTools: data?.templates.filter(
      (template) => template.kind === PromptTemplateKind.HigherOrderTool,
    ),
    isLoading,
  };
}
