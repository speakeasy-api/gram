import { RequireScope } from "@/components/require-scope";
import { HooksContent } from "@/components/observe/HooksContent";

export default function HooksPage() {
  return (
    <RequireScope scope="project:read" level="page">
      <HooksContent />
    </RequireScope>
  );
}
