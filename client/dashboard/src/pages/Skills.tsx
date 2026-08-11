import { RequireScope } from "@/components/require-scope";
import { useProject } from "@/contexts/Auth";
import { Outlet } from "react-router";

export default function Skills(): JSX.Element {
  const { id: projectId } = useProject();
  return (
    <RequireScope scope="skill:read" resourceId={projectId} level="page">
      <Outlet />
    </RequireScope>
  );
}
