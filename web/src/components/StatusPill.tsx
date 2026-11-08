import type { ReactNode } from "react";
import "./system.css";

export type StatusTone = "ok" | "warn" | "danger" | "info";

/** Shapes are fixed per state (DESIGN.md Shapes): ● ok, ○ stale or pending, ■ running, maintenance or danger. */
const SHAPE: Record<StatusTone, string> = { ok: "●", warn: "○", danger: "■", info: "■" };

/** Status pill: shape plus text, never color alone. */
export function StatusPill({ tone, children }: { tone: StatusTone; children: ReactNode }) {
  return (
    <span className={`hx-status-pill hx-status-pill--${tone}`}>
      <span className="hx-status-shape" aria-hidden="true">
        {SHAPE[tone]}
      </span>
      <span>{children}</span>
    </span>
  );
}
