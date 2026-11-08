/**
 * Time values travel and persist as UTC RFC 3339 (AGENTS.md); the UI shows them in the user's time zone.
 */

interface DurationFormatLike {
  format(duration: Partial<Record<"days" | "hours" | "minutes" | "seconds", number>>): string;
}
type DurationFormatCtor = new (
  locale: string,
  options: { style: "short"; secondsDisplay?: "always" | "auto" },
) => DurationFormatLike;

export type DateStyle = "datetime" | "date" | "time";

/**
 * Parses an RFC 3339 timestamp (`2026-11-08T03:42:06Z`, `2026-11-08T11:42:06.120+08:00`).
 * Returns null for anything that is not a complete, valid RFC 3339 date-time with an offset.
 */
export function parseRfc3339(value: unknown): Date | null {
  if (typeof value !== "string") return null;
  const m = /^(\d{4})-(\d{2})-(\d{2})[Tt](\d{2}):(\d{2}):(\d{2})(?:\.(\d+))?(?:([Zz])|([+-])(\d{2}):(\d{2}))$/.exec(
    value,
  );
  if (!m) return null;
  const [year, month, day, hour, minute, second] = m.slice(1, 7).map(Number) as [
    number,
    number,
    number,
    number,
    number,
    number,
  ];
  if (hour > 23 || minute > 59 || second > 59) return null;
  let offsetMinutes = 0;
  if (m[8] === undefined) {
    const offHour = Number(m[10]);
    const offMinute = Number(m[11]);
    if (offHour > 23 || offMinute > 59) return null;
    offsetMinutes = (m[9] === "-" ? -1 : 1) * (offHour * 60 + offMinute);
  }
  const ms = Number((m[7] ?? "").slice(0, 3).padEnd(3, "0"));
  const date = new Date(0);
  date.setUTCFullYear(year, month - 1, day);
  if (date.getUTCFullYear() !== year || date.getUTCMonth() !== month - 1 || date.getUTCDate() !== day) return null;
  date.setUTCHours(hour, minute, second, ms);
  return new Date(date.getTime() - offsetMinutes * 60_000);
}

/** Serializes an instant as UTC RFC 3339 with a `Z` suffix; milliseconds only when non-zero. */
export function toRfc3339Utc(date: Date): string {
  return date.toISOString().replace(".000Z", "Z");
}

/**
 * Formats an RFC 3339 value for display in `timeZone` using `locale` (BCP 47, e.g. "zh-Hant-TW", "en").
 * 24-hour clock, numeric parts. Returns null when the value cannot be parsed, so callers render
 * "unknown" instead of a fake time.
 */
export function formatDateTime(
  value: string,
  locale: string,
  timeZone: string,
  style: DateStyle = "datetime",
): string | null {
  const date = parseRfc3339(value);
  if (!date) return null;
  const options: Intl.DateTimeFormatOptions = { timeZone, hourCycle: "h23" };
  if (style !== "time") Object.assign(options, { year: "numeric", month: "2-digit", day: "2-digit" });
  if (style !== "date") Object.assign(options, { hour: "2-digit", minute: "2-digit", second: "2-digit" });
  // Some ICU versions separate date and time with U+2009 (thin space) in zh-Hant-TW; use a plain space.
  return new Intl.DateTimeFormat(locale, options).format(date).replaceAll("\u2009", " ");
}

/**
 * Elapsed time from `fromIso` to `now`, in the two largest non-zero units (days, hours, minutes, seconds),
 * localized for `locale`. Returns null when `fromIso` cannot be parsed or lies in the future.
 */
export function formatElapsed(fromIso: string, now: Date, locale: string): string | null {
  const from = parseRfc3339(fromIso);
  if (!from) return null;
  const total = Math.floor((now.getTime() - from.getTime()) / 1000);
  if (total < 0) return null;
  const parts = [
    ["days", Math.floor(total / 86400)],
    ["hours", Math.floor(total / 3600) % 24],
    ["minutes", Math.floor(total / 60) % 60],
    ["seconds", total % 60],
  ] as const;
  const first = parts.findIndex(([, n]) => n > 0);
  const duration: Partial<Record<(typeof parts)[number][0], number>> = {};
  if (first === -1) {
    duration.seconds = 0;
  } else {
    const [unit, n] = parts[first]!;
    duration[unit] = n;
    const next = parts[first + 1];
    if (next && next[1] > 0) duration[next[0]] = next[1];
  }
  const { DurationFormat } = Intl as unknown as { DurationFormat?: DurationFormatCtor };
  if (DurationFormat) {
    return new DurationFormat(locale, { style: "short", secondsDisplay: first === -1 ? "always" : "auto" }).format(
      duration,
    );
  }
  // Browsers without Intl.DurationFormat (Chrome < 129, Firefox < 136): format each unit on its own.
  const units = { days: "day", hours: "hour", minutes: "minute", seconds: "second" } as const;
  return (Object.keys(units) as (keyof typeof units)[])
    .filter((k) => duration[k] !== undefined)
    .map((k) =>
      new Intl.NumberFormat(locale, { style: "unit", unit: units[k], unitDisplay: "short" }).format(duration[k]!),
    )
    .join(" ");
}

/** The browser's IANA time zone, falling back to "UTC" when the runtime reports none. */
export function browserTimeZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
}
