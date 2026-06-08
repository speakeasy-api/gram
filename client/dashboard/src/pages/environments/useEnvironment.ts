import { useParams } from "react-router";
import { useEnvironments } from "./useEnvironments";

type EnvironmentItem = ReturnType<typeof useEnvironments>[number];

export function useEnvironment(slug?: string):
  | (EnvironmentItem & {
      refetch: ReturnType<typeof useEnvironments>["refetch"];
    })
  | null {
  let { environmentSlug } = useParams();
  if (slug) environmentSlug = slug;

  const environments = useEnvironments();

  const environment = environments.find(
    (environment) => environment.slug === environmentSlug,
  );

  return environment
    ? Object.assign(environment, { refetch: environments.refetch })
    : null;
}
