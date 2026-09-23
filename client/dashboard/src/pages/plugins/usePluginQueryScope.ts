import { useProject, useSession } from "@/contexts/Auth";

/** Include identity and project in SDK cache keys, not only request headers. */
export function usePluginQueryScope() {
  const project = useProject();
  const session = useSession();
  return { gramProject: project.slug, gramSession: session.session };
}
