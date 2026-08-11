import type { JSX } from "react";
import { Routes, Route, Navigate } from "react-router";
import { AdminLayout } from "@/layouts/AdminLayout";
import { OrganizationsList } from "@/pages/OrganizationsList";
import { OrganizationDetail } from "@/pages/OrganizationDetail";
import { ProjectLookup } from "@/pages/ProjectLookup";
import { ProjectDetail } from "@/pages/ProjectDetail";

// The Gram admin API owns /admin/* on this origin, so the SPA claims the rest.
export function App(): JSX.Element {
  return (
    <Routes>
      <Route element={<AdminLayout />}>
        <Route path="/" element={<Navigate to="/organizations" replace />} />
        <Route path="/organizations" element={<OrganizationsList />} />
        <Route
          path="/organizations/:idOrSlug"
          element={<OrganizationDetail />}
        />
        <Route path="/projects" element={<ProjectLookup />} />
        <Route path="/projects/:idOrSlug" element={<ProjectDetail />} />
        <Route path="*" element={<Navigate to="/organizations" replace />} />
      </Route>
    </Routes>
  );
}
