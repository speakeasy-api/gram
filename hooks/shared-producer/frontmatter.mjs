function parseTruthyYamlValue(rawValue) {
  const noComment = rawValue.split("#", 1)[0]?.trim() ?? "";
  const unquoted = noComment
    .replace(/^['"]|['"]$/g, "")
    .trim()
    .toLowerCase();
  return unquoted === "true";
}

function extractFrontmatter(content) {
  const match = content.match(/^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)/);
  return match ? match[1] : null;
}

function normalizeFrontmatterLine(line) {
  return line.replace(/\r$/, "");
}

function isRegistryManagedMetadataLine(trimmedLine) {
  return (
    /^metadata\.skill_uuid\s*:/i.test(trimmedLine) ||
    /^metadata\.x-gram-[\w-]+\s*:/i.test(trimmedLine)
  );
}

function splitFrontmatterLine(line) {
  const indent = (line.match(/^\s*/) ?? [""])[0].length;
  return {
    line,
    trimmed: line.trim(),
    indent,
  };
}

function isRegistryManagedMetadataChild(trimmedLine) {
  return (
    /^x-gram-[\w-]+\s*:/i.test(trimmedLine) ||
    /^skill_uuid\s*:/i.test(trimmedLine)
  );
}

export function stripRegistryManagedFrontmatter(content) {
  const match = content.match(
    /^(---\r?\n)([\s\S]*?)(\r?\n---(?:\r?\n|$))([\s\S]*)$/,
  );
  if (!match) {
    return content;
  }

  const [, start, rawFrontmatter, end, body] = match;
  const lines = rawFrontmatter.split(/\r?\n/).map(normalizeFrontmatterLine);

  const cleaned = [];

  for (let i = 0; i < lines.length; ) {
    const current = splitFrontmatterLine(lines[i]);

    if (!current.trimmed) {
      cleaned.push(current.line);
      i += 1;
      continue;
    }

    if (isRegistryManagedMetadataLine(current.trimmed)) {
      i += 1;
      continue;
    }

    const metadataMatch = current.line.match(/^(\s*)metadata\s*:\s*(.*)$/i);
    if (!metadataMatch) {
      cleaned.push(current.line);
      i += 1;
      continue;
    }

    const metadataIndent = metadataMatch[1].length;
    const inlineValue = metadataMatch[2].trim();
    const metadataLines = [];
    let hasNonManagedChild = false;

    let j = i + 1;
    while (j < lines.length) {
      const next = splitFrontmatterLine(lines[j]);
      if (!next.trimmed) {
        metadataLines.push(next.line);
        j += 1;
        continue;
      }

      if (next.indent <= metadataIndent) {
        break;
      }

      if (isRegistryManagedMetadataChild(next.trimmed)) {
        j += 1;
        continue;
      }

      hasNonManagedChild = true;
      metadataLines.push(next.line);
      j += 1;
    }

    const keepInlineMetadata =
      inlineValue.length > 0 && !isRegistryManagedMetadataChild(inlineValue);

    if (hasNonManagedChild || keepInlineMetadata) {
      cleaned.push(current.line);
      if (hasNonManagedChild) {
        cleaned.push(...metadataLines);
      }
    }

    i = j;
  }

  return `${start}${cleaned.join("\n")}${end}${body}`;
}

export function hasXGramIgnoreFrontmatter(content) {
  const frontmatter = extractFrontmatter(content);
  if (!frontmatter) {
    return false;
  }

  const lines = frontmatter.split(/\r?\n/);

  for (const line of lines) {
    const dotted = line.trim().match(/^metadata\.x-gram-ignore\s*:\s*(.+)$/i);
    if (dotted && parseTruthyYamlValue(dotted[1])) {
      return true;
    }
  }

  for (let i = 0; i < lines.length; i += 1) {
    const line = lines[i];
    const metadata = line.match(/^(\s*)metadata\s*:\s*(.*)$/i);
    if (!metadata) {
      continue;
    }

    const metadataIndent = metadata[1].length;
    const inlineValue = metadata[2].trim();
    if (inlineValue && /x-gram-ignore\s*:\s*(.+)/i.test(inlineValue)) {
      const inlineMatch = inlineValue.match(/x-gram-ignore\s*:\s*(.+)/i);
      if (inlineMatch && parseTruthyYamlValue(inlineMatch[1])) {
        return true;
      }
    }

    for (let j = i + 1; j < lines.length; j += 1) {
      const nestedLine = lines[j];
      const trimmed = nestedLine.trim();

      if (!trimmed) {
        continue;
      }

      const nestedIndent = (nestedLine.match(/^\s*/) ?? [""])[0].length;
      if (nestedIndent <= metadataIndent) {
        break;
      }

      const xGramIgnore = trimmed.match(/^x-gram-ignore\s*:\s*(.+)$/i);
      if (xGramIgnore && parseTruthyYamlValue(xGramIgnore[1])) {
        return true;
      }
    }
  }

  return false;
}
