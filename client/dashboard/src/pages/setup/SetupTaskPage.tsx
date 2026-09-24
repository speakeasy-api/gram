import {
  Navigate,
  useLocation,
  useParams,
  useSearchParams,
} from "react-router";
import { useOrgRoutes } from "@/routes";
import { canonicalSetupSearch } from "./task-slugs";

export default function SetupTaskPage(): JSX.Element {
  const { taskSlug } = useParams();
  const [search] = useSearchParams();
  const location = useLocation();
  const routes = useOrgRoutes();
  return (
    <Navigate
      replace
      to={{
        pathname: routes.setup.href(),
        search: `?${canonicalSetupSearch(search, taskSlug)}`,
        hash: location.hash,
      }}
    />
  );
}
