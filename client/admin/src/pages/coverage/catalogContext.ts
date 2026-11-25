import { createContext, useContext } from "react";
import type { Catalog } from "./model";

export const CatalogContext = createContext<Catalog | null>(null);
export function useCatalog(): Catalog {
  const catalog = useContext(CatalogContext);
  if (!catalog) throw new Error("Support catalog is not loaded");
  return catalog;
}
