import type { Theme } from "./types";

const MODE_KEY = "dsh.themeMode";
let currentMode: Theme = "system";
const media = typeof window !== "undefined" && typeof window.matchMedia === "function"
  ? window.matchMedia("(prefers-color-scheme: dark)")
  : null;

export function normalizeTheme(value?: string): Theme {
  if (value === "light" || value === "dark" || value === "system") return value;
  return "system";
}

function resolve(mode: Theme): "light" | "dark" {
  if (mode === "light" || mode === "dark") return mode;
  return media?.matches ? "dark" : "light";
}

export function applyTheme(value?: string, cache = false): "light" | "dark" {
  const mode = normalizeTheme(value);
  currentMode = mode;
  const resolved = resolve(mode);
  if (typeof document !== "undefined") {
    document.documentElement.dataset.theme = resolved;
    document.documentElement.dataset.themeMode = mode;
    document.documentElement.style.colorScheme = resolved;
  }
  if (cache) {
    try { localStorage.setItem(MODE_KEY, mode); } catch { /* private/embedded storage is optional */ }
  }
  return resolved;
}

export function currentThemeMode(): Theme {
  return currentMode;
}

if (media) {
  const onChange = () => { if (currentMode === "system") applyTheme("system"); };
  if (typeof media.addEventListener === "function") media.addEventListener("change", onChange);
  else media.addListener(onChange);
}
