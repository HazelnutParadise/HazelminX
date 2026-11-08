import { CommandPalette } from "@cloudflare/kumo/components/command-palette";
import { MagnifyingGlassIcon } from "@phosphor-icons/react";
import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { useNavigate } from "react-router";
import { PAGES, type PageKey } from "../app/navigation";

interface PageItem {
  key: PageKey;
  title: string;
  path: string;
}

const IS_MAC = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);

/**
 * ⌘K / Ctrl+K search. This ticket searches page names only; resource search with permission
 * filtering arrives with ticket 017 (UI-004).
 */
export function CommandSearch() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setOpen(true);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const items = useMemo<PageItem[]>(() => {
    const q = query.trim().toLowerCase();
    return Object.values(PAGES)
      .map((p) => ({ key: p.key, title: t(`nav.${p.key}`), path: p.path }))
      .filter((p) => !q || p.title.toLowerCase().includes(q) || p.path.includes(q));
  }, [query, t]);

  const go = (item: PageItem) => {
    setOpen(false);
    setQuery("");
    void navigate(item.path);
  };

  const shortcut = IS_MAC ? "⌘K" : "Ctrl K";

  return (
    <>
      <button
        type="button"
        className="hx-search"
        onClick={() => setOpen(true)}
        aria-keyshortcuts={IS_MAC ? "Meta+K" : "Control+K"}
      >
        <MagnifyingGlassIcon size={16} aria-hidden="true" />
        <span className="hx-search-text">{t("shell.search")}</span>
        <kbd className="hx-kbd" aria-hidden="true">
          {shortcut}
        </kbd>
      </button>
      <CommandPalette.Root
        open={open}
        onOpenChange={setOpen}
        items={items}
        value={query}
        onValueChange={setQuery}
        itemToStringValue={(item: PageItem) => item.title}
        onSelect={(item: PageItem) => go(item)}
        getSelectableItems={(list: PageItem[]) => list}
      >
        <CommandPalette.Input placeholder={t("shell.search")} aria-label={t("shell.search")} />
        <CommandPalette.List>
          <CommandPalette.Results>
            {(item: PageItem) => {
              const Icon = PAGES[item.key].icon;
              return (
                <CommandPalette.Item key={item.key} value={item} onClick={() => go(item)}>
                  <span className="hx-cmd-item">
                    <Icon size={16} aria-hidden="true" />
                    <span>{item.title}</span>
                  </span>
                </CommandPalette.Item>
              );
            }}
          </CommandPalette.Results>
          <CommandPalette.Empty>{t("shell.searchEmpty")}</CommandPalette.Empty>
        </CommandPalette.List>
      </CommandPalette.Root>
    </>
  );
}
