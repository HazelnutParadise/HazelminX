import { describe, it, expect, vi } from "vite-plus/test";
import { browserTimeZone, formatDateTime, formatElapsed, parseRfc3339, toRfc3339Utc } from "./time";

// 期望字串皆來自 node 24（Intl ICU）實際輸出，例如：
//   new Intl.DateTimeFormat("en", { timeZone: "Asia/Taipei", year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit", hourCycle: "h23" }).format(new Date("2026-11-08T20:00:00Z"))
//   => "11/09/2026, 04:00:00"

describe("parseRfc3339", () => {
  it("parses a UTC timestamp with Z", () => {
    expect(parseRfc3339("2026-11-08T03:42:06Z")?.getTime()).toBe(Date.UTC(2026, 10, 8, 3, 42, 6));
  });

  it("parses fractional seconds with a numeric offset", () => {
    expect(parseRfc3339("2026-11-08T11:42:06.120+08:00")?.getTime()).toBe(Date.UTC(2026, 10, 8, 3, 42, 6, 120));
  });

  it("accepts lowercase t and z", () => {
    expect(parseRfc3339("2026-11-08t03:42:06z")?.getTime()).toBe(Date.UTC(2026, 10, 8, 3, 42, 6));
  });

  it.each([
    "2026-11-08",
    "2026-11-08T03:42:06",
    "2026-11-08 03:42:06Z",
    "2026-02-30T00:00:00Z",
    "2026-13-01T00:00:00Z",
    "2026-11-08T24:00:00Z",
    "2026-11-08T23:59:60Z",
    "2026-11-08T03:42:06+25:00",
    "not a date",
    "",
  ])("returns null for the string %j", (value) => {
    expect(parseRfc3339(value)).toBeNull();
  });

  it.each([
    ["number", 1731037326],
    ["null", null],
    ["undefined", undefined],
  ])("returns null for a non-string value (%s)", (_label, value) => {
    expect(parseRfc3339(value)).toBeNull();
  });
});

describe("toRfc3339Utc", () => {
  it("serializes whole seconds without milliseconds", () => {
    expect(toRfc3339Utc(new Date(Date.UTC(2026, 10, 8, 3, 42, 6)))).toBe("2026-11-08T03:42:06Z");
  });

  it("serializes milliseconds when they are non-zero", () => {
    expect(toRfc3339Utc(new Date(Date.UTC(2026, 10, 8, 3, 42, 6, 120)))).toBe("2026-11-08T03:42:06.120Z");
  });

  it("round-trips through parseRfc3339", () => {
    const d = new Date(Date.UTC(2026, 10, 8, 3, 42, 6, 120));
    expect(parseRfc3339(toRfc3339Utc(d))?.getTime()).toBe(d.getTime());
  });

  it("throws RangeError for an invalid date", () => {
    expect(() => toRfc3339Utc(new Date(NaN))).toThrow(RangeError);
  });
});

describe("formatDateTime", () => {
  it("formats the time part in the given zone", () => {
    expect(formatDateTime("2026-11-08T03:42:06Z", "en", "Asia/Taipei", "time")).toBe("11:42:06");
  });

  it("formats the time part in UTC", () => {
    expect(formatDateTime("2026-11-08T03:42:06Z", "en", "UTC", "time")).toBe("03:42:06");
  });

  it("uses the datetime style by default", () => {
    expect(formatDateTime("2026-11-08T03:42:06Z", "en", "UTC")).toBe("11/08/2026, 03:42:06");
  });

  // 2026-11-08T20:00:00Z 在 Asia/Taipei 已是 11/09 04:00:00
  it.each([
    ["en", "date", "11/09/2026"],
    ["en", "datetime", "11/09/2026, 04:00:00"],
    ["en", "time", "04:00:00"],
    ["zh-Hant-TW", "date", "2026/11/09"],
    ["zh-Hant-TW", "datetime", "2026/11/09 04:00:00"],
    ["zh-Hant-TW", "time", "04:00:00"],
  ] as const)("formats %s %s in Asia/Taipei across the date boundary", (locale, style, expected) => {
    expect(formatDateTime("2026-11-08T20:00:00Z", locale, "Asia/Taipei", style)).toBe(expected);
  });

  it("formats an instant before the zone boundary in zh-Hant-TW", () => {
    expect(formatDateTime("2026-11-08T03:42:06Z", "zh-Hant-TW", "Asia/Taipei")).toBe("2026/11/08 11:42:06");
  });

  it.each(["bad", "2026-11-08"])("returns null for the unparseable value %j", (value) => {
    expect(formatDateTime(value, "en", "UTC")).toBeNull();
  });

  it("throws RangeError for an invalid time zone instead of swallowing it", () => {
    expect(() => formatDateTime("2026-11-08T03:42:06Z", "en", "Mars/Base")).toThrow(RangeError);
  });
});

describe("formatElapsed", () => {
  const now = new Date("2026-11-08T03:42:06Z");

  it("shows minutes and seconds in en", () => {
    expect(formatElapsed("2026-11-08T03:30:00Z", now, "en")).toBe("12 min, 6 sec");
  });

  it("shows minutes and seconds in zh-Hant-TW", () => {
    expect(formatElapsed("2026-11-08T03:30:00Z", now, "zh-Hant-TW")).toBe("12 分鐘 6 秒");
  });

  it("shows zero seconds instead of an empty string when no time has passed", () => {
    expect(formatElapsed("2026-11-08T03:42:06Z", now, "en")).toBe("0 sec");
    expect(formatElapsed("2026-11-08T03:42:06Z", now, "zh-Hant-TW")).toBe("0 秒");
  });

  it("keeps only the two largest units: 1 day 1 hour 1 min 1 sec becomes 1 day, 1 hr", () => {
    expect(formatElapsed("2026-11-07T02:41:05Z", now, "en")).toBe("1 day, 1 hr");
    expect(formatElapsed("2026-11-07T02:41:05Z", now, "zh-Hant-TW")).toBe("1 天 1 小時");
  });

  it("shows only the hour for exactly 3600 seconds", () => {
    expect(formatElapsed("2026-11-08T02:42:06Z", now, "en")).toBe("1 hr");
    expect(formatElapsed("2026-11-08T02:42:06Z", now, "zh-Hant-TW")).toBe("1 小時");
  });

  it("shows only the hour when minutes are zero, even if seconds are not", () => {
    expect(formatElapsed("2026-11-08T02:42:01Z", now, "en")).toBe("1 hr");
    expect(formatElapsed("2026-11-08T02:42:01Z", now, "zh-Hant-TW")).toBe("1 小時");
  });

  it("falls back to unit formatting when Intl.DurationFormat is missing (older browsers)", () => {
    const saved = (Intl as unknown as Record<string, unknown>).DurationFormat;
    delete (Intl as unknown as Record<string, unknown>).DurationFormat;
    try {
      const unit = (u: string, n: number, l: string) =>
        new Intl.NumberFormat(l, { style: "unit", unit: u, unitDisplay: "short" }).format(n);
      expect(formatElapsed("2026-11-08T03:30:00Z", now, "en")).toBe(
        `${unit("minute", 12, "en")} ${unit("second", 6, "en")}`,
      );
      expect(formatElapsed("2026-11-08T03:42:06Z", now, "zh-Hant-TW")).toBe(unit("second", 0, "zh-Hant-TW"));
    } finally {
      (Intl as unknown as Record<string, unknown>).DurationFormat = saved;
    }
  });

  it("returns null when fromIso is after now", () => {
    expect(formatElapsed("2026-11-08T03:42:07Z", now, "en")).toBeNull();
  });

  it("returns null when fromIso cannot be parsed", () => {
    expect(formatElapsed("bad", now, "en")).toBeNull();
  });
});

describe("browserTimeZone", () => {
  it("returns the time zone reported by Intl", () => {
    expect(browserTimeZone()).toBe(Intl.DateTimeFormat().resolvedOptions().timeZone);
  });

  it('falls back to "UTC" when the runtime reports no time zone', () => {
    const spy = vi
      .spyOn(Intl.DateTimeFormat.prototype, "resolvedOptions")
      .mockImplementation(() => ({ timeZone: undefined }) as unknown as Intl.ResolvedDateTimeFormatOptions);
    try {
      expect(browserTimeZone()).toBe("UTC");
    } finally {
      spy.mockRestore();
    }
  });
});
