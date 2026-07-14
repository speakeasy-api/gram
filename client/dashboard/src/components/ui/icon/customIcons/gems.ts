import { IconNode } from "lucide-react";
import createCustomLucideIcon from "./createCustomLucideIcon";

const iconNode: IconNode = [
  [
    "path",
    {
      d: "M156.139 157.469L196.307 117.301L168.177 89.1397V89.0126H88.2885L88.4113 89.2582L88.2885 89.1354L60.6496 117.296L128.5 184.929",
      key: "path-1",
    },
  ],
  [
    "path",
    {
      d: "M128.5 14L29 71.0835V185.25L128.5 242.333L228 185.25V71.0829L128.5 14ZM208.96 174.341L128.5 220.767L48.0401 174.34V81.7386L128.5 35.3114L208.96 81.3762",
      key: "path-2",
    },
  ],
];

const icon = createCustomLucideIcon("gems", iconNode, {
  viewBox: "0 0 256 256",
  fill: "currentColor",
});

export { icon as default };
