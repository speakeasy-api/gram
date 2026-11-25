const MDM_DOCS_BASE =
  "https://www.speakeasy.com/docs/ai-control-plane/reference/device-agent/mdm-installations";

export interface MdmTarget {
  id: string;
  name: string;
  platforms: string;
  note: string;
  logo: string;
  /** Monochrome marks vanish on a dark background; flip those. */
  invertLogoInDark?: boolean;
}

// Mirrors the targets the MDM installations reference covers. Compliance
// destinations on the same page (Drata, Vanta) are not rollout paths, so
// they are left out.
export const MDM_TARGETS: MdmTarget[] = [
  {
    id: "jamf",
    name: "Jamf Pro",
    platforms: "macOS",
    note: "Inventory integration supported",
    logo: "/icons/mdm/jamf.png",
  },
  {
    id: "iru",
    name: "Iru (formerly Kandji)",
    platforms: "macOS",
    note: "Inventory integration supported",
    logo: "/icons/mdm/iru.png",
  },
  {
    id: "intune",
    name: "Microsoft Intune",
    platforms: "macOS, Windows",
    note: "Inventory integration supported",
    logo: "/icons/mdm/intune.png",
  },
  {
    id: "manageengine-endpoint-central",
    name: "ManageEngine Endpoint Central",
    platforms: "macOS, Windows",
    note: "Deployment only",
    logo: "/icons/mdm/manageengine.png",
  },
  {
    id: "linux",
    name: "Linux",
    platforms: "Ansible, Puppet, or any configuration management",
    note: "No inventory integration",
    logo: "/icons/platforms/linux.svg",
  },
];

export function mdmGuideUrl(target: Pick<MdmTarget, "id">): string {
  return `${MDM_DOCS_BASE}/${target.id}`;
}
