import { Monitor, Moon, Sun } from "lucide-react";

import { type Theme, useThemeStore } from "../lib/theme";
import { Button } from "./ui";

const order: Theme[] = ["light", "dark", "system"];
const icons: Record<Theme, typeof Sun> = { light: Sun, dark: Moon, system: Monitor };
const labels: Record<Theme, string> = {
  light: "Light theme",
  dark: "Dark theme",
  system: "System theme",
};

// Cycles light → dark → system. Replaces the previously inert Sun button in the
// header. Upgrades to a dropdown once shadcn's DropdownMenu lands.
export function ThemeToggle() {
  const theme = useThemeStore((s) => s.theme);
  const setTheme = useThemeStore((s) => s.setTheme);
  const Icon = icons[theme];
  const next = order[(order.indexOf(theme) + 1) % order.length];

  return (
    <Button
      variant="secondary"
      size="icon"
      onClick={() => setTheme(next)}
      title={`${labels[theme]} — click for ${labels[next].toLowerCase()}`}
      aria-label={`Switch theme (current: ${labels[theme].toLowerCase()})`}
    >
      <Icon className="h-4 w-4" aria-hidden="true" />
    </Button>
  );
}
