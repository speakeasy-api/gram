import { getServerURL } from "@/lib/utils";
import { useSession } from "./Auth";
import { useProject } from "./Auth";

export const useFetcher = (): {
  fetch: (endpoint: string, opts: RequestInit) => Promise<Response>;
} => {
  const project = useProject();
  const { session } = useSession();

  const f = (endpoint: string, opts: RequestInit) =>
    fetch(`${getServerURL()}${endpoint}`, {
      ...opts,
      headers: {
        "gram-project": project.slug,
        ...(opts.headers as Record<string, string> | undefined),
        "gram-session": session,
      },
    });

  return { fetch: f };
};
