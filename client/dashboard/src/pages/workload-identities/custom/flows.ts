import type { WorkloadCustomFlows } from "@gram/client/models/components/workloadcustomflows.js";
import type { WorkloadForm } from "@gram/client/models/components/workloadform.js";
import type { WorkloadFormBlock } from "@gram/client/models/components/workloadformblock.js";
import { useWorkloadCustomFlows } from "@gram/client/react-query/workloadCustomFlows.js";
import { useMemo } from "react";
import { optional } from "../setup/platforms";
import type { CustomFlows, CustomForm, FormBlock } from "./definition";

/**
 * The custom flows the server ships, as the renderer's types. The server
 * validates them when it loads, so this only reshapes: an empty optional field
 * becomes absent, a block keeps only the fields its type uses, and a block
 * this dashboard cannot render is dropped.
 */
export function useCustomFlows(): {
  flows: CustomFlows | undefined;
  isPending: boolean;
  isError: boolean;
  refetch: () => void;
} {
  // The flows change only when the server is deployed.
  const query = useWorkloadCustomFlows({}, undefined, {
    throwOnError: false,
    staleTime: Infinity,
  });
  const flows = useMemo(
    () => (query.data === undefined ? undefined : toCustomFlows(query.data)),
    [query.data],
  );
  return {
    flows,
    isPending: query.isPending,
    isError: query.isError,
    refetch: () => void query.refetch(),
  };
}

function toCustomFlows(flows: WorkloadCustomFlows): CustomFlows {
  return {
    registerPlatform: toCustomForm(flows.registerPlatform),
    editPlatform: toCustomForm(flows.editPlatform),
    allowAccess: toCustomForm(flows.allowAccess),
    editAccess: toCustomForm(flows.editAccess),
  };
}

function toCustomForm(form: WorkloadForm): CustomForm {
  return {
    title: form.title,
    description: form.description,
    submitLabel: form.submitLabel,
    pendingLabel: form.pendingLabel,
    steps: form.steps.map((step) => ({
      id: step.id,
      title: step.title,
      blocks: step.blocks.flatMap(toFormBlock),
    })),
  };
}

function toFormBlock(block: WorkloadFormBlock): FormBlock[] {
  switch (block.type) {
    case "text":
      return [{ type: "text", markdown: block.markdown }];
    case "link":
      return [{ type: "link", href: block.href, label: block.label }];
    case "input":
      if (block.field === "" || block.format === "") return [];
      return [
        {
          type: "input",
          field: block.field,
          format: block.format,
          label: block.label,
          placeholder: optional(block.placeholder),
          help: optional(block.help),
          multiline: block.multiline,
          readOnly: block.readOnly,
        },
      ];
    case "agent_picker":
    case "tags":
      return [
        {
          type: block.type,
          label: block.label,
          placeholder: optional(block.placeholder),
          help: optional(block.help),
        },
      ];
    case "wildcard_caution":
      return [{ type: "wildcard_caution" }];
    default:
      return [];
  }
}
