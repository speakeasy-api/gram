import { useParams } from "react-router";
import { useEnvironments } from "./useEnvironments";

export function useEnvironment(slug?: string) {
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
