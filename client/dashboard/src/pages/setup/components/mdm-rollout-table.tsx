import { ExternalLink } from "lucide-react";
import { Column, Table } from "@/components/ui/Table";
import { MDM_TARGETS, mdmGuideUrl, type MdmTarget } from "./mdm-targets";

function GuideLink({
  target,
  children,
  className,
}: {
  target: MdmTarget;
  children: React.ReactNode;
  className?: string;
}): JSX.Element {
  return (
    <a
      href={mdmGuideUrl(target)}
      target="_blank"
      rel="noopener noreferrer"
      className={className}
    >
      {children}
    </a>
  );
}

const columns: Column<MdmTarget>[] = [
  {
    key: "target",
    header: "MDM",
    render: (target) => (
      <GuideLink
        target={target}
        className="hover:text-foreground flex items-center gap-3"
      >
        <span className="bg-card border-border flex h-8 w-8 flex-shrink-0 items-center justify-center border">
          <img
            src={target.logo}
            alt=""
            aria-hidden
            className={
              target.invertLogoInDark
                ? "h-5 w-5 object-contain dark:invert"
                : "h-5 w-5 object-contain"
            }
          />
        </span>
        <span className="text-foreground text-sm font-medium">
          {target.name}
        </span>
      </GuideLink>
    ),
  },
  {
    key: "platforms",
    header: "Platforms",
    render: (target) => (
      <span className="text-muted-foreground text-sm">{target.platforms}</span>
    ),
  },
  {
    key: "note",
    header: "Notes",
    render: (target) => (
      <span className="text-muted-foreground text-sm">{target.note}</span>
    ),
  },
  {
    key: "guide",
    header: "",
    width: "140px",
    render: (target) => (
      <GuideLink
        target={target}
        className="text-foreground inline-flex items-center gap-1.5 text-sm underline underline-offset-2"
      >
        Open guide
        <ExternalLink className="h-3.5 w-3.5" />
      </GuideLink>
    ),
  },
];

// One row per MDM the device agent has a rollout guide for; every link opens
// the guide in a new tab so the admin keeps their place in setup.
export function MdmRolloutTable(): JSX.Element {
  return (
    <Table
      columns={columns}
      data={MDM_TARGETS}
      rowKey={(target) => target.id}
    />
  );
}
