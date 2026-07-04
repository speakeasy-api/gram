/** A member attribute a spend-control target condition can be written against.
 *  The server turns the selected attribute/operator/value into the stored CEL
 *  expression; this file is only UI reference data. */
export interface ActorAttribute {
  name: string;
  type: "string" | "list";
  description: string;
  /** Representative values, surfaced in the editor reference. */
  samples: string[];
}

/**
 * The actor attributes available to spend-control rules. Rules target
 * organization members: identity (email), org roles (roles), and — when the
 * org syncs a directory — the WorkOS directory attributes Gram already
 * ingests (department_name, job_title, employee_type, division_name,
 * cost_center_name) plus group memberships (groups). Keep in sync with the
 * server CEL environment in server/internal/spendrules/celenv.
 */
export const ACTOR_ATTRIBUTES: ActorAttribute[] = [
  {
    name: "department_name",
    type: "string",
    description: "Directory department the actor belongs to.",
    samples: ["Engineering", "Data Science", "Design", "Support", "Finance"],
  },
  {
    name: "job_title",
    type: "string",
    description: "Directory job title.",
    samples: ["Software Engineer", "Staff Engineer", "Manager", "Analyst"],
  },
  {
    name: "employee_type",
    type: "string",
    description: "Employment classification.",
    samples: ["full_time", "contractor", "intern"],
  },
  {
    name: "division_name",
    type: "string",
    description: "Directory division / business unit.",
    samples: ["R&D", "Platform", "Go-To-Market"],
  },
  {
    name: "cost_center_name",
    type: "string",
    description: "Finance cost center the actor rolls up to.",
    samples: ["CC-1001", "CC-2043", "CC-3100"],
  },
  {
    name: "email",
    type: "string",
    description: "Member email address.",
    samples: ["ada@acme.com", "grace@acme.com"],
  },
  {
    name: "groups",
    type: "list",
    description: "IdP / directory group memberships.",
    samples: ["eng-frontier", "ml-team", "interns", "leadership"],
  },
  {
    name: "roles",
    type: "list",
    description: "Organization role slugs the member holds.",
    samples: ["admin", "member"],
  },
];
