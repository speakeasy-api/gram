/** Quote every field and keep spreadsheet applications from treating text as formulas. */
export function matrixCsv(rows: string[][]): string {
  return (
    "\uFEFF" +
    rows
      .map((row) =>
        row
          .map((value) => {
            const text =
              /^[\s]*[=+@-]/.test(value) || /^[\t\r\n]/.test(value)
                ? `'${value}`
                : value;
            return `"${text.replaceAll('"', '""')}"`;
          })
          .join(","),
      )
      .join("\r\n") +
    "\r\n"
  );
}

export function downloadMatrixCsv(csv: string, filename: string): void {
  const url = URL.createObjectURL(
    new Blob([csv], { type: "text/csv;charset=utf-8" }),
  );
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.append(anchor);
  anchor.click();
  anchor.remove();
  // Give the browser time to start reading the download before releasing its URL.
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
