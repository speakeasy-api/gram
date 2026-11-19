import { useProject, useSession } from "@/contexts/Auth";
import { getChatMcpBaseURL } from "@/lib/utils";
import { createOpenRouter } from "@openrouter/ai-sdk-provider";

/** A model for one-off dashboard completions, billed to the current project. */
export const useOpenRouterModel = (
  model: string,
): ReturnType<ReturnType<typeof createOpenRouter>["chat"]> => {
  const session = useSession();
  const project = useProject();

  const openrouter = createOpenRouter({
    apiKey: "this is required",
    baseURL: getChatMcpBaseURL(),
    headers: {
      "Gram-Session": session.session,
      "Gram-Project": project.slug,
    },
  });

  return openrouter.chat(model);
};
