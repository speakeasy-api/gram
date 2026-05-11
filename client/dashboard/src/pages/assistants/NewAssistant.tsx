import { useEffect } from "react";
import { useSearchParams } from "react-router";
import { useSidebar } from "@/components/ui/sidebar-context";
import { NewAssistantOnboarding } from "./onboarding/AssistantOnboarding";

export default function NewAssistantPage() {
  const { setOpen } = useSidebar();
  const [searchParams, setSearchParams] = useSearchParams();

  useEffect(() => {
    if (searchParams.get("disposition") !== "assistants") return;
    setOpen(false);
    const next = new URLSearchParams(searchParams);
    next.delete("disposition");
    setSearchParams(next, { replace: true });
  }, [searchParams, setOpen, setSearchParams]);

  return <NewAssistantOnboarding />;
}
