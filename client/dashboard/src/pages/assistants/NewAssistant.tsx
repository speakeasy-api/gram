import { useEffect } from "react";
import { useSearchParams } from "react-router";
import { useSidebar } from "@/components/ui/Sidebar/sidebar-context";
import { NewAssistantOnboarding } from "./onboarding/AssistantOnboarding";
import { RequireScope } from "@/components/require-scope";
import { useProject } from "@/contexts/Auth";

export default function NewAssistantPage(): JSX.Element {
  const project = useProject();
  const { setOpen } = useSidebar();
  const [searchParams, setSearchParams] = useSearchParams();

  useEffect(() => {
    if (searchParams.get("disposition") !== "assistants") return;
    setOpen(false);
    const next = new URLSearchParams(searchParams);
    next.delete("disposition");
    setSearchParams(next, { replace: true });
  }, [searchParams, setOpen, setSearchParams]);

  return (
    <RequireScope scope="assistant:write" resourceId={project.id} level="page">
      <NewAssistantOnboarding />
    </RequireScope>
  );
}
