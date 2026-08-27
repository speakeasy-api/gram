export type EditableWriteOnlyHeader = {
  rowID: string;
  name: string;
  storedName: string | null;
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
    storedName: header.hasValue ? header.name : null,
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

export function hasValidWriteOnlyHeaders(
  headers: EditableWriteOnlyHeader[],
): boolean {
  return headers.every((header) => {
    if (header.name.trim() === "") return false;
    return header.value !== "" || preservesStoredHeaderValue(header);
  });
}

export function writeOnlyHeaderInput(
  header: EditableWriteOnlyHeader,
): WriteOnlyHeaderInput {
  const name = header.name.trim();
  if (preservesStoredHeaderValue(header)) return { name };
  return { name, value: header.value };
}
