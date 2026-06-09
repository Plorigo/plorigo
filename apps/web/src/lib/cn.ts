import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";

// cn merges conditional class lists (clsx) and resolves conflicting Tailwind
// utilities so the last one wins (tailwind-merge). shadcn/ui components expect
// this helper; the rest of the app imports it from here too.
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
