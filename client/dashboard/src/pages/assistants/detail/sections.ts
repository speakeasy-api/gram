export type SectionName = "Personality" | "Behavior" | "Tasks";

const HEADING_RE = /^# (Personality|Behavior|Tasks)[ \t]*\r?$/gm;

type SectionRange = {
  name: SectionName;
  start: number;
  contentStart: number;
  end: number;
};

function findSections(text: string): SectionRange[] {
  HEADING_RE.lastIndex = 0;
  const found: Omit<SectionRange, "end">[] = [];
  for (let m: RegExpExecArray | null; (m = HEADING_RE.exec(text));) {
    found.push({
      name: m[1] as SectionName,
      start: m.index,
      contentStart: m.index + m[0].length,
    });
  }
  return found.map((r, i) => ({
    ...r,
    end: found[i + 1]?.start ?? text.length,
  }));
}

export function setSection(
  text: string,
  name: SectionName,
  content: string,
): string {
  const trimmed = content.trim();
  const block = trimmed.length > 0 ? `# ${name}\n${trimmed}` : `# ${name}`;
  const ranges = findSections(text);
  const r = ranges.find((x) => x.name === name);
  if (r) {
    const before = text.slice(0, r.start).replace(/\s+$/, "");
    const after = text.slice(r.end).replace(/^\s+/, "");
    return [before, block, after].filter((x) => x.length > 0).join("\n\n");
  }
  const head = text.replace(/\s+$/, "");
  if (head.length === 0) return block;
  return `${head}\n\n${block}`;
}

export type InstructionParts = {
  /** Text before the first known heading: older or hand-written prompts. */
  preamble: string;
  Personality: string;
  Behavior: string;
  Tasks: string;
};

/** Splits instructions into the sections the editor shows. */
export function splitInstructions(text: string): InstructionParts {
  const ranges = findSections(text);
  const parts: InstructionParts = {
    preamble: text.slice(0, ranges[0]?.start ?? text.length).trim(),
    Personality: "",
    Behavior: "",
    Tasks: "",
  };
  // A heading that repeats keeps every block, so saving loses nothing.
  for (const r of ranges) {
    const body = text.slice(r.contentStart, r.end).trim();
    parts[r.name] = [parts[r.name], body].filter(Boolean).join("\n\n");
  }
  return parts;
}

/** Joins edited sections back, in a fixed order, skipping empty ones. */
export function joinInstructions(parts: InstructionParts): string {
  const blocks = [parts.preamble.trim()];
  for (const name of ["Personality", "Tasks", "Behavior"] as const) {
    const body = parts[name].trim();
    if (body) blocks.push(`# ${name}\n${body}`);
  }
  return blocks.filter((b) => b.length > 0).join("\n\n");
}
