/** Bytes as people read them (1.2 KiB). */
export function bytes(n: number | null | undefined): string {
  const v = n ?? 0;
  if (v < 1024) return `${v} B`;
  if (v < 1024 * 1024) return `${(v / 1024).toFixed(1)} KiB`;
  return `${(v / 1024 / 1024).toFixed(1)} MiB`;
}

/** i18n keys for a finding, plan check or blocker code. */
export const findingKeys = (code: string | null | undefined) => [
  `projects.code.finding.${code ?? ""}`,
  `projects.code.check.${code ?? ""}`,
];
