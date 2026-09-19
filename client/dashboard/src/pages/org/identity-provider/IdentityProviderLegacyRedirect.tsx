import { Navigate, useLocation, useParams } from "react-router";

import {
  legacyIdentityProviderSearch,
  legacyOktaTab,
} from "./identityProviderQueries";

/** `/<org>/okta?tab=<sub>` moved onto the Enterprise Managed Auth's Okta workspace. */
export default function IdentityProviderLegacyRedirect(): JSX.Element {
  const { orgSlug } = useParams();
  const location = useLocation();
  const search = new URLSearchParams(location.search);
  const canonicalSearch = legacyIdentityProviderSearch(
    search,
    legacyOktaTab(search.get("tab")),
  );
  return (
    <Navigate
      to={`/${orgSlug}/identity${canonicalSearch}${location.hash}`}
      replace
    />
  );
}
