import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import {
  BellIcon,
  GlobeIcon,
  LockKeyIcon,
  MoonIcon,
  SignOutIcon,
  SunIcon,
  TerminalWindowIcon,
  UserCircleIcon,
} from "@phosphor-icons/react";
import { useTranslation } from "react-i18next";
import { Link, useNavigate } from "react-router";
import { PAGES } from "../app/navigation";
import { useSignOut } from "../auth/session";
import { useShellStatus } from "../app/shellStatus";
import { StatusPill } from "../components/StatusPill";
import { activeLocale, LANGUAGES, setLanguage, type LanguageCode } from "../i18n";
import { browserTimeZone, formatDateTime, parseRfc3339 } from "../lib/time";
import { setMode, useMode } from "../prefs/mode";
import { CommandSearch } from "./CommandSearch";
import { useNow } from "./useNow";

/** Observation older than this gets a warning pill (DESIGN.md Layout). */
const STALE_AFTER_SECONDS = 40;

function Observed() {
  const { t } = useTranslation();
  const { observedAt, refreshSeconds } = useShellStatus();
  const now = useNow(5_000);
  const at = observedAt ? parseRfc3339(observedAt) : null;
  const time = at && observedAt ? formatDateTime(observedAt, activeLocale(), browserTimeZone(), "time") : null;
  const stale = at !== null && now.getTime() - at.getTime() > STALE_AFTER_SECONDS * 1000;
  return (
    <span className="hx-obs">
      {t("shell.observed")}{" "}
      {time ? (
        <time className="hx-mono" dateTime={observedAt}>
          {time}
        </time>
      ) : (
        <span className="hx-mono">—</span>
      )}
      {time && refreshSeconds ? ` · ${t("shell.refreshEvery", { seconds: refreshSeconds })}` : null}
      {stale && <StatusPill tone="warn">{t("shell.stale", { seconds: STALE_AFTER_SECONDS })}</StatusPill>}
    </span>
  );
}

function AttentionLink() {
  const { t } = useTranslation();
  const { attention } = useShellStatus();
  const shown = attention !== undefined && attention > 0;
  const label = shown
    ? `${t("shell.attention")} · ${t("shell.attentionCount", { count: attention })}`
    : t("shell.attention");
  return (
    <Link className="hx-ib" to={PAGES.events.path} aria-label={label} title={label}>
      <BellIcon size={18} aria-hidden="true" />
      {shown && (
        <b className="hx-badge hx-badge--danger" aria-hidden="true">
          {attention > 99 ? "99+" : attention}
        </b>
      )}
    </Link>
  );
}

function ThemeButton() {
  const { t } = useTranslation();
  const mode = useMode();
  const label = mode === "dark" ? t("shell.toLight") : t("shell.toDark");
  return (
    <button
      type="button"
      className="hx-ib"
      onClick={() => setMode(mode === "dark" ? "light" : "dark")}
      aria-label={label}
      title={label}
    >
      {mode === "dark" ? <SunIcon size={18} aria-hidden="true" /> : <MoonIcon size={18} aria-hidden="true" />}
    </button>
  );
}

function LanguageItems() {
  const { i18n } = useTranslation();
  return (
    <DropdownMenu.RadioGroup value={i18n.language} onValueChange={(v: string) => void setLanguage(v as LanguageCode)}>
      {LANGUAGES.map((l) => (
        <DropdownMenu.RadioItem key={l.code} value={l.code} lang={l.htmlLang}>
          {l.label}
          <DropdownMenu.RadioItemIndicator />
        </DropdownMenu.RadioItem>
      ))}
    </DropdownMenu.RadioGroup>
  );
}

/** Signed-out top bar: language gets its own menu because there is no account menu. */
function LanguageMenu() {
  const { t } = useTranslation();
  return (
    <DropdownMenu>
      <DropdownMenu.Trigger
        render={
          <button type="button" className="hx-ib" aria-label={t("shell.language")} title={t("shell.language")}>
            <GlobeIcon size={18} aria-hidden="true" />
          </button>
        }
      />
      <DropdownMenu.Content align="end">
        <LanguageItems />
      </DropdownMenu.Content>
    </DropdownMenu>
  );
}

function UserMenu() {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const { user } = useShellStatus();
  const label = user ? `${user.name} · ${user.role}` : t("shell.account");
  const languageLabel = LANGUAGES.find((l) => l.code === i18n.language)?.label ?? "";
  const signOut = useSignOut();
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
        {user && (
          <div className="hx-menu-head">
            <strong>{user.name}</strong>
            <span>{[user.role, user.email].filter(Boolean).join(" · ")}</span>
          </div>
        )}
        <DropdownMenu.Item icon={LockKeyIcon} onClick={() => void navigate(PAGES.accountSecurity.path)}>
          {t("shell.accountSecurity")}
        </DropdownMenu.Item>
        <DropdownMenu.Item icon={TerminalWindowIcon} onClick={() => void navigate(PAGES.terminal.path)}>
          {t("shell.terminal")}
        </DropdownMenu.Item>
        <DropdownMenu.Sub>
          <DropdownMenu.SubTrigger icon={GlobeIcon}>
            {t("shell.languageCurrent", { language: languageLabel })}
          </DropdownMenu.SubTrigger>
          <DropdownMenu.SubContent>
            <LanguageItems />
          </DropdownMenu.SubContent>
        </DropdownMenu.Sub>
        {user && (
          <>
            <DropdownMenu.Separator />
            <DropdownMenu.Item icon={SignOutIcon} onClick={() => void signOut()}>
              {t("shell.signOut")}
            </DropdownMenu.Item>
          </>
        )}
      </DropdownMenu.Content>
    </DropdownMenu>
  );
}

function Brand({ collapsed }: { collapsed: boolean }) {
  const { t } = useTranslation();
  return (
    <Link className={collapsed ? "hx-brand hx-brand--mini" : "hx-brand"} to="/" aria-label={t("app.home")}>
      <span className="hx-logo" aria-hidden="true" />
      <span className="hx-brand-name">{t("app.name")}</span>
    </Link>
  );
}

/**
 * Top bar (DESIGN.md Layout): brand, ⌘K search, observation time, needs-attention bell, terminal,
 * theme, account. Running jobs are counted on the sidebar's 工作中心, not here. Phones keep the bell,
 * theme and account on the first row (terminal moves into the account menu) and put search on row two.
 */
export function TopBar({ collapsed = false }: { collapsed?: boolean }) {
  const { t } = useTranslation();
  const { signedOut } = useShellStatus();

  if (signedOut) {
    return (
      <header className="hx-top">
        <Brand collapsed={false} />
        <div className="hx-tright">
          <ThemeButton />
          <LanguageMenu />
        </div>
      </header>
    );
  }

  return (
    <header className="hx-top">
      <Brand collapsed={collapsed} />
      <CommandSearch />
      <div className="hx-tright">
        <Observed />
        <AttentionLink />
        <Link
          className="hx-ib hx-terminal"
          to={PAGES.terminal.path}
          aria-label={t("shell.terminal")}
          title={t("shell.terminal")}
        >
          <TerminalWindowIcon size={18} aria-hidden="true" />
        </Link>
        <ThemeButton />
        <UserMenu />
      </div>
    </header>
  );
}
