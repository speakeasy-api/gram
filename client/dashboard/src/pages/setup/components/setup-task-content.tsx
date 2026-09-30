import { RequireScope } from "@/components/require-scope";
import { useOrganization } from "@/contexts/Auth";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { setupCard, type SetupCardProps } from "../setup-cards";
import { StepSupportProvider } from "./step-container";

const handoff = (
  <p>
    Ask an organization administrator with the required product permissions to
    complete this setup.
  </p>
);

/** Renders a setup card behind the permissions its registry entry names. */
export function SetupTaskContent({
  taskKey,
  onSupport,
  ...props
}: Omit<SetupCardProps, "projectSlug"> & {
  taskKey: string;
  onSupport: () => void;
}): JSX.Element | null {
  const organization = useOrganization();
  const requestProjectSlug = useProjectSlugForRequests();
  const card = setupCard(taskKey);
  if (!card) return null;

  let content = <card.Step {...props} projectSlug={requestProjectSlug} />;
  if (card.projectScopes) {
    const project = organization.projects.find(
      (candidate) => candidate.slug === requestProjectSlug,
    );
    if (!project) return handoff;
    content = (
      <RequireScope
        scope={card.projectScopes}
        all
        resourceId={project.id}
        level="section"
        fallback={handoff}
      >
        {content}
      </RequireScope>
    );
  }

  return (
    <StepSupportProvider onSupport={onSupport}>
      <RequireScope
        scope="org:admin"
        resourceId={organization.id}
        level="section"
        fallback={handoff}
      >
        {content}
      </RequireScope>
    </StepSupportProvider>
  );
}
