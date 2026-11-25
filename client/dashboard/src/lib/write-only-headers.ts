export type EditableWriteOnlyHeader = {
  rowID: string;
  name: string;
  storedName: string | null;
  hasStoredValue: boolean;
  value: string;
};

export type WriteOnlyHeaderSummary = {
  name: string;
  hasValue: boolean;
};

export type WriteOnlyHeaderInput = {
  name: string;
  value?: string;
};

export function editableHeaderFromServer(
  header: WriteOnlyHeaderSummary,
  index: number,
): EditableWriteOnlyHeader {
  return {
    rowID: `existing-${index}-${header.name}`,
    name: header.name,
    storedName: header.name,
    hasStoredValue: header.hasValue,
    value: "",
  };
}

let newRowCounter = 0;

export function blankWriteOnlyHeader(): EditableWriteOnlyHeader {
  newRowCounter += 1;
  return {
    rowID: `new-${newRowCounter}`,
    name: "",
    storedName: null,
    hasStoredValue: false,
    value: "",
  };
}

export function preservesStoredHeaderValue(
  header: EditableWriteOnlyHeader,
): boolean {
  return (
    header.storedName !== null &&
    header.value === "" &&
    header.name.trim().toLowerCase() === header.storedName.toLowerCase()
  );
}

const HTTP_HEADER_NAME_PATTERN = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]+$/;
function hasInvalidHTTPHeaderValue(value: string): boolean {
  for (let index = 0; index < value.length; index += 1) {
    const code = value.charCodeAt(index);
    if ((code < 0x20 && code !== 0x09) || code === 0x7f) return true;
  }
  return false;
}

export function hasValidWriteOnlyHeaders(
  headers: EditableWriteOnlyHeader[],
): boolean {
  const names = new Set<string>();
  return headers.every((header) => {
    const name = header.name.trim();
    const foldedName = name.toLowerCase();
    if (!HTTP_HEADER_NAME_PATTERN.test(name) || names.has(foldedName)) {
      return false;
    }
    names.add(foldedName);

    if (preservesStoredHeaderValue(header)) return true;
    return header.value !== "" && !hasInvalidHTTPHeaderValue(header.value);
  });
}

export function writeOnlyHeaderInput(
  header: EditableWriteOnlyHeader,
): WriteOnlyHeaderInput {
  const name = header.name.trim();
  if (preservesStoredHeaderValue(header)) return { name };
  return { name, value: header.value };
}
