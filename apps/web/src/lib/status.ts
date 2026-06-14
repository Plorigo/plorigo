// Single source of truth for status/intent colors. Components map their legacy
// tone names (green/amber/red/blue/purple/neutral) to a semantic intent, then
// pull token-based classes from here — so nothing hardcodes emerald/amber/etc.
// and everything themes (light/dark) from index.css.

export type Intent = "neutral" | "success" | "warning" | "danger" | "info" | "violet";
export type Tone = "neutral" | "green" | "amber" | "red" | "blue" | "purple";

export const toneToIntent: Record<Tone, Intent> = {
  neutral: "neutral",
  green: "success",
  amber: "warning",
  red: "danger",
  blue: "info",
  purple: "violet",
};

// Solid fill — status dots, meter bars, sparklines.
export const intentDot: Record<Intent, string> = {
  neutral: "bg-muted-foreground",
  success: "bg-success",
  warning: "bg-warning",
  danger: "bg-destructive",
  info: "bg-info",
  violet: "bg-chart-4",
};

// Solid text color — values that sit on a card/canvas.
export const intentText: Record<Intent, string> = {
  neutral: "text-muted-foreground",
  success: "text-success",
  warning: "text-warning",
  danger: "text-destructive",
  info: "text-info",
  violet: "text-chart-4",
};

// Subtle chip/badge/icon-tile: faint tinted background + colored text + border.
export const intentSoft: Record<Intent, string> = {
  neutral: "border-border bg-muted text-muted-foreground",
  success: "border-success/20 bg-success/10 text-success",
  warning: "border-warning/20 bg-warning/10 text-warning",
  danger: "border-destructive/20 bg-destructive/10 text-destructive",
  info: "border-info/20 bg-info/10 text-info",
  violet: "border-chart-4/20 bg-chart-4/10 text-chart-4",
};

export const intentToTone: Record<Intent, Tone> = {
  neutral: "neutral",
  success: "green",
  warning: "amber",
  danger: "red",
  info: "blue",
  violet: "purple",
};

// Maps free-form status strings (deploy/server/domain states) to a legacy tone,
// for components like StatusDot/Badge that take a tone.
export function statusTone(status: string): Tone {
  return intentToTone[statusIntent(status)];
}

// Maps free-form status strings (deploy/server/domain states) to an intent.
export function statusIntent(status: string): Intent {
  if (["ready", "healthy", "online", "passing", "active", "success", "running"].includes(status)) {
    return "success";
  }
  if (["building", "queued", "protected", "pending", "info", "assigned", "pulling", "starting", "routing"].includes(status)) {
    return "info";
  }
  if (["degraded", "needs restore test", "warning", "warn"].includes(status)) {
    return "warning";
  }
  if (["failed", "offline", "unavailable", "attention", "error", "danger"].includes(status)) {
    return "danger";
  }
  return "neutral";
}
