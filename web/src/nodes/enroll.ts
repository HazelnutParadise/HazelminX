import type { NodeMatch } from "./api";

/** The command shown under 「在主機上執行」(agent/crates/agent/src/main.rs `enroll`). */
export function enrollCommand(coreUrl: string, code: string, group: string): string {
  const parts = ["hazelminx-agent enroll", `--core-url ${coreUrl.trim()}`, `--code ${code}`];
  if (group.trim()) parts.push(`--group ${shellQuote(group.trim())}`);
  return parts.join(" \\\n  ");
}

function shellQuote(s: string): string {
  return /^[A-Za-z0-9._@%+=:,/-]+$/.test(s) ? s : `'${s.replace(/'/g, `'\\''`)}'`;
}

/** What approving would do with the existing hosts this one resembles. */
interface Decision {
  blocked: NodeMatch | null;
  sameKey: NodeMatch | null;
  machine: NodeMatch[];
}

export function decide(matches: NodeMatch[]): Decision {
  const blocked = matches.find((m) => m.keyRetired) ?? null;
  const sameKey = matches.find((m) => m.reason === "same_key" && !m.keyRetired) ?? null;
  const machine = sameKey ? [] : matches.filter((m) => m.reason === "same_machine_id" && !m.keyRetired);
  return { blocked, sameKey, machine };
}
