import { useSlugs } from "@/contexts/Sdk";
import { Navigate, useLocation } from "react-router";

export function LegacyDataRedirect(): JSX.Element {
  const { orgSlug } = useSlugs();
  const location = useLocation();

  return (
    <Navigate
      to={`/${orgSlug}/data/event-feed${location.search}${location.hash}`}
      replace
    />
  );
}
