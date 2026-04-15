import { Gram } from "@gram/client";
import { HTTPClient } from "@gram/client/lib/http.js";
import { getServerURL } from "./utils";

export function createDashboardGramClient(
  projectSlug = getProjectSlugFromPath(),
) {
  const httpClient = new HTTPClient({
    fetcher: (request) => {
      const nextRequest = new Request(request, {
        credentials: "include",
      });

      if (projectSlug && !nextRequest.headers.get("gram-project")) {
        nextRequest.headers.set("gram-project", projectSlug);
      }

      return fetch(nextRequest);
    },
  });

  return new Gram({
    serverURL: getServerURL(),
    httpClient,
  });
}

export function getProjectSlugFromPath(): string | null {
  if (typeof window === "undefined") {
    return null;
  }

  const parts = window.location.pathname.split("/").filter(Boolean);
  if (parts[1] !== "projects" || parts.length < 3) {
    return null;
  }

  return parts[2] ?? null;
}
