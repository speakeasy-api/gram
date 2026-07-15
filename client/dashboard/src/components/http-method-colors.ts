export const HttpMethodColors: Record<
  string,
  { bg: string; text: string; border: string }
> = {
  GET: {
    bg: "bg-information-default!",
    text: "text-default-information!",
    border: "border-information-default!",
  },
  POST: {
    bg: "bg-success-default!",
    text: "text-default-success!",
    border: "border-success-default!",
  },
  PATCH: {
    bg: "bg-warning-default!",
    text: "text-default-warning!",
    border: "border-warning-default!",
  },
  PUT: {
    bg: "bg-warning-default!",
    text: "text-default-warning!",
    border: "border-warning-default!",
  },
  DELETE: {
    bg: "bg-destructive-default!",
    text: "text-default-destructive!",
    border: "border-destructive-default!",
  },
};
