import { useSyncExternalStore } from "react";
import { PREF_KEYS, readPref, writePref } from "./storage";

export type Mode = "dark" | "light";

/** Dark is the default theme (DESIGN.md Overview). */
export function savedMode(): Mode {
  return readPref(PREF_KEYS.mode) === "light" ? "light" : "dark";
}

const listeners = new Set<() => void>();

function current(): Mode {
  return document.documentElement.getAttribute("data-mode") === "light" ? "light" : "dark";
}

/** Applies the mode to <html data-mode> (Kumo reads it) and remembers it. */
export function setMode(mode: Mode): void {
  document.documentElement.setAttribute("data-mode", mode);
  writePref(PREF_KEYS.mode, mode);
  for (const l of listeners) l();
}

export function useMode(): Mode {
  return useSyncExternalStore(
    (cb) => {
      listeners.add(cb);
      return () => listeners.delete(cb);
    },
    current,
    () => "dark",
  );
}
