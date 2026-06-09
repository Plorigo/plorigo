// Shared formatting helpers used across feature pages.

export function formatDate(value?: string): string {
  if (!value) {
    return "recently";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "recently";
  }
  return new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" }).format(date);
}

export function errorMessage(error: unknown): string {
  if (!error) {
    return "";
  }
  return error instanceof Error ? error.message : "Could not load this data";
}

// Reads a percentage out of a label like "28%" or "4.1 / 16 GB" for meters.
export function percentFromLabel(value: string): number {
  const directPercent = value.match(/(\d+)%/);
  if (directPercent) {
    return Number(directPercent[1]);
  }
  const fraction = value.match(/([\d.]+)\s*\/\s*([\d.]+)/);
  if (fraction) {
    return Math.min(100, Math.round((Number(fraction[1]) / Number(fraction[2])) * 100));
  }
  return 0;
}
