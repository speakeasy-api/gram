import { useOrganization, useProject } from "@/contexts/Auth";
import { Navigate, useLocation, useParams } from "react-router";

/** Preserve bookmarks and external consent links to the former org pages. */
export default function ProjectIdentityRedirect(): JSX.Element {
  const project = useProject();
  const organization = useOrganization();
  const { orgSlug } = useParams();
  const location = useLocation();
  const search = new URLSearchParams(location.search);
  const requestedSlug = search.get("project") || search.get("projectSlug");
  const projectSlug =
    organization.projects.find((entry) => entry.slug === requestedSlug)?.slug ??
    project.slug;
  search.delete("project");
  search.delete("projectSlug");
  const suffix = location.pathname.split("/").slice(2).join("/");
  const query = search.toString();
  return (
    <Navigate
      to={`/${orgSlug}/projects/${encodeURIComponent(projectSlug)}/${suffix}${query ? `?${query}` : ""}${location.hash}`}
      replace
    />
  );
}
