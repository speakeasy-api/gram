export type CollectionVisibility = "public" | "private";

export interface CollectionServer {
  registrySpecifier: string;
  title: string;
  description: string;
  iconUrl?: string;
  toolCount: number;
}

export interface Collection {
  id: string;
  name: string;
  slug?: string;
  description: string;
  visibility: CollectionVisibility;
  iconUrl?: string;
  servers: CollectionServer[];
  author: { orgName: string; orgId: string };
  installCount: number;
  createdAt: string;
  updatedAt: string;
}
