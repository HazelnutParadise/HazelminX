import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { BellIcon, ListChecksIcon, MoonIcon, SunIcon, TerminalWindowIcon, UserCircleIcon } from "@phosphor-icons/react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router";
import { PAGES } from "../app/navigation";
import { useShellStatus } from "../app/shellStatus";
import { activeLocale, LANGUAGES, setLanguage, type LanguageCode } from "../i18n";
import { browserTimeZone, formatDateTime } from "../lib/time";
import { setMode, useMode } from "../prefs/mode";
import { CommandSearch } from "./CommandSearch";

function Observed() {
  const { t } = useTranslation();
  const { observedAt, refreshSeconds } = useShellStatus();
  const time = observedAt ? formatDateTime(observedAt, activeLocale(), browserTimeZone(), "time") : null;
  return (
    <span className="hx-obs">
      {time ? (
        <>
          {t("shell.observed")}{" "}
          <time className="hx-mono" dateTime={observedAt}>
            {time}
          </time>
          {refreshSeconds ? ` · ${t("shell.refreshEvery", { seconds: refreshSeconds })}` : null}
        </>
      ) : (
        t("shell.observedUnknown")
      )}
    </span>
  );
}

function CountLink({
  to,
  icon: Icon,
  label,
  count,
  tone,
}: {
  to: string;
  icon: typeof BellIcon;
  label: string;
  count?: number;
  tone: "danger" | "hazel";
}) {
  const shown = count !== undefined && count > 0;
  return (
    <Link className="hx-ib" to={to} aria-label={label} title={label}>
      <Icon size={18} aria-hidden="true" />
      {shown && (
        <b className={`hx-badge hx-badge--${tone}`} aria-hidden="true">
          {count > 99 ? "99+" : count}
        </b>
      )}
    </Link>
  );
}

function UserMenu() {
  const { t, i18n } = useTranslation();
  const { user } = useShellStatus();
  const label = user ? `${user.name} · ${user.role}` : t("shell.account");
  return (
    <DropdownMenu>
      <DropdownMenu.Trigger
        render={
          <button type="button" className="hx-ib hx-user" aria-label={label}>
            <UserCircleIcon size={18} aria-hidden="true" />
            {user && <span className="hx-user-name">{user.name}</span>}
          </button>
        }
      />
      <DropdownMenu.Content align="end">
        <DropdownMenu.Group>
          <DropdownMenu.Label>{t("shell.language")}</DropdownMenu.Label>
          <DropdownMenu.RadioGroup
            value={i18n.language}
            onValueChange={(v: string) => void setLanguage(v as LanguageCode)}
          >
            {LANGUAGES.map((l) => (
              <DropdownMenu.RadioItem key={l.code} value={l.code} lang={l.htmlLang}>
                {l.label}
                <DropdownMenu.RadioItemIndicator />
              </DropdownMenu.RadioItem>
            ))}
          </DropdownMenu.RadioGroup>
        </DropdownMenu.Group>
      </DropdownMenu.Content>
    </DropdownMenu>
  );
}

export function TopBar() {
  const { t } = useTranslation();
  const status = useShellStatus();
  const mode = useMode();
  const attentionLabel =
    status.attention !== undefined && status.attention > 0
      ? `${t("shell.attention")} · ${t("shell.attentionCount", { count: status.attention })}`
      : t("shell.attention");
  const jobsLabel =
    status.jobs !== undefined && status.jobs > 0
      ? `${t("shell.jobs")} · ${t("shell.jobsCount", { count: status.jobs })}`
      : t("shell.jobs");

  return (
    <header className="hx-top">
      <Link className="hx-brand" to="/" aria-label={t("app.home")}>
        <span className="hx-logo" aria-hidden="true" />
        <span className="hx-brand-name">{t("app.name")}</span>
      </Link>
      <CommandSearch />
      <div className="hx-tright">
        <Observed />
        <CountLink
          to={PAGES.events.path}
          icon={BellIcon}
          label={attentionLabel}
          count={status.attention}
          tone="danger"
        />
        <CountLink to={PAGES.jobs.path} icon={ListChecksIcon} label={jobsLabel} count={status.jobs} tone="hazel" />
        <Link className="hx-ib" to={PAGES.terminal.path} aria-label={t("shell.terminal")} title={t("shell.terminal")}>
          <TerminalWindowIcon size={18} aria-hidden="true" />
        </Link>
        <button
          type="button"
          className="hx-ib"
          onClick={() => setMode(mode === "dark" ? "light" : "dark")}
          aria-label={mode === "dark" ? t("shell.toLight") : t("shell.toDark")}
          title={mode === "dark" ? t("shell.toLight") : t("shell.toDark")}
        >
          {mode === "dark" ? <SunIcon size={18} aria-hidden="true" /> : <MoonIcon size={18} aria-hidden="true" />}
        </button>
        <UserMenu />
      </div>
    </header>
  );
}
