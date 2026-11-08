import { act, fireEvent, render, screen, within } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router";
import { beforeAll, describe, expect, it, vi } from "vite-plus/test";
import { ApiError } from "../api/client";
import { ShellStatusContext, type ShellStatus } from "../app/shellStatus";
import { initI18n } from "../i18n";
import { NavCount } from "../shell/NavCount";
import { ComponentDownBanner } from "./ComponentDownBanner";
import { HostBar } from "./HostBar";
import { ApiErrorMessage, RawMessage } from "./RawMessage";
import { SaveBar } from "./SaveBar";

beforeAll(() => {
  initI18n("zh-TW");
});

const withRouter = (ui: ReactNode) => render(<MemoryRouter>{ui}</MemoryRouter>);

describe("HostBar", () => {
  const hosts = ["a", "b", "c", "d"].map((id) => ({ id, name: `host-${id}` }));

  it("renders nothing when every host responds", () => {
    const { container } = withRouter(<HostBar hosts={[]} allHref="/nodes" hostHref={(id) => `/nodes/${id}`} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("names the first three hosts, counts the rest and links to the filtered list", () => {
    withRouter(<HostBar hosts={hosts} allHref="/nodes?status=unreachable" hostHref={(id) => `/nodes/${id}`} />);
    const bar = screen.getByRole("status", { name: "主機狀態" });
    expect(within(bar).getByText("4 台主機沒有回應")).toBeInTheDocument();
    for (const id of ["a", "b", "c"]) {
      expect(within(bar).getByRole("link", { name: `host-${id}` })).toHaveAttribute("href", `/nodes/${id}`);
    }
    expect(within(bar).queryByRole("link", { name: "host-d" })).toBeNull();
    expect(within(bar).getByText("另外 1 台")).toBeInTheDocument();
    expect(within(bar).getByRole("link", { name: "全部主機" })).toHaveAttribute("href", "/nodes?status=unreachable");
    expect(within(bar).queryByRole("button", { name: /關閉|close/i })).toBeNull();
  });
});

describe("SaveBar", () => {
  const props = { effect: "存檔後要重建 shop-api 才會生效", saveLabel: "儲存並重建 shop-api" };

  it("is absent without unsaved changes", () => {
    const { container } = render(<SaveBar changes={0} {...props} onSave={vi.fn()} onDiscard={vi.fn()} />);
    expect(container).toBeEmptyDOMElement();
  });

  it("states the count and the effect, and wires discard and save", () => {
    const onSave = vi.fn();
    const onDiscard = vi.fn();
    render(<SaveBar changes={3} {...props} onSave={onSave} onDiscard={onDiscard} />);
    const bar = screen.getByRole("region", { name: "未儲存的變更" });
    expect(within(bar).getByText("3 處未儲存")).toBeInTheDocument();
    expect(bar).toHaveTextContent(props.effect);
    fireEvent.click(within(bar).getByRole("button", { name: "捨棄" }));
    fireEvent.click(within(bar).getByRole("button", { name: /儲存並重建 shop-api/ }));
    expect(onDiscard).toHaveBeenCalledTimes(1);
    expect(onSave).toHaveBeenCalledTimes(1);
  });

  it("saves on Ctrl+S and ⌘S, but not while a save is running", () => {
    const onSave = vi.fn();
    const { rerender } = render(<SaveBar changes={1} {...props} onSave={onSave} onDiscard={vi.fn()} />);
    const ctrl = new KeyboardEvent("keydown", { key: "s", ctrlKey: true, cancelable: true });
    window.dispatchEvent(ctrl);
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "s", metaKey: true, cancelable: true }));
    expect(onSave).toHaveBeenCalledTimes(2);
    expect(ctrl.defaultPrevented).toBe(true);
    rerender(<SaveBar changes={1} {...props} saving onSave={onSave} onDiscard={vi.fn()} />);
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "s", ctrlKey: true, cancelable: true }));
    expect(onSave).toHaveBeenCalledTimes(2);
  });

  it("leaves Ctrl+S alone when there is nothing to save", () => {
    const onSave = vi.fn();
    render(<SaveBar changes={0} {...props} onSave={onSave} onDiscard={vi.fn()} />);
    const ev = new KeyboardEvent("keydown", { key: "s", ctrlKey: true, cancelable: true });
    window.dispatchEvent(ev);
    expect(onSave).not.toHaveBeenCalled();
    expect(ev.defaultPrevented).toBe(false);
  });

  it("ignores auto-repeat and IME composition so holding ⌘S saves once", () => {
    const onSave = vi.fn();
    render(<SaveBar changes={1} {...props} onSave={onSave} onDiscard={vi.fn()} />);
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "s", metaKey: true, cancelable: true }));
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "s", metaKey: true, repeat: true, cancelable: true }));
    window.dispatchEvent(
      new KeyboardEvent("keydown", { key: "s", metaKey: true, isComposing: true, cancelable: true }),
    );
    expect(onSave).toHaveBeenCalledTimes(1);
  });

  it("asks before the page unloads while changes are unsaved", () => {
    render(<SaveBar changes={2} {...props} onSave={vi.fn()} onDiscard={vi.fn()} />);
    const ev = new Event("beforeunload", { cancelable: true });
    window.dispatchEvent(ev);
    expect(ev.defaultPrevented).toBe(true);
  });
});

describe("RawMessage", () => {
  const raw =
    "/etc/ssh/sshd_config line 42: Bad configuration option: PermitRootLogn\n  terminating, 1 bad configuration options";

  it("shows the plain summary and keeps the original text verbatim behind a disclosure", () => {
    const { container } = render(<RawMessage summary="sshd 設定檢查沒有通過" raw={raw} source="sshd -t" />);
    expect(screen.getByText("sshd 設定檢查沒有通過")).toBeInTheDocument();
    const details = container.querySelector("details")!;
    expect(details.open).toBe(false);
    expect(details.querySelector("summary")).toHaveTextContent("原文 · 來源：sshd -t");
    const pre = details.querySelector("pre")!;
    expect(pre.textContent).toBe(raw);
    expect(pre).toHaveAttribute("translate", "no");
  });

  it("hides the original block when there is no original text", () => {
    const { container } = render(<RawMessage summary="ok" raw="" />);
    expect(container.querySelector("details")).toBeNull();
  });

  it("turns a known API error code into its plain summary and keeps the body and trace id", () => {
    const body = '{"code":"FORBIDDEN","message":"missing platform:write","traceId":"7f3a"}';
    const { container } = render(
      <ApiErrorMessage
        error={
          new ApiError({
            status: 403,
            code: "FORBIDDEN",
            message: "missing platform:write",
            raw: body,
            traceId: "7f3a",
          })
        }
      />,
    );
    expect(screen.getByText("你的角色沒有這個操作的權限。")).toBeInTheDocument();
    expect(container.querySelector("pre")!.textContent).toBe(body);
    expect(container.querySelector("summary")).toHaveTextContent("trace 7f3a");
  });

  it("falls back to the HTTP status for codes without a translation", () => {
    render(
      <ApiErrorMessage error={new ApiError({ status: 418, code: "TEAPOT", message: "short and stout", raw: "" })} />,
    );
    expect(screen.getByText("Core 回傳錯誤（HTTP 418）。")).toBeInTheDocument();
    expect(document.querySelector("pre")!.textContent).toBe("short and stout");
  });
});

describe("ComponentDownBanner", () => {
  const base = {
    name: "Zentinel",
    kind: "systemd" as const,
    host: "hazel-tw-01",
    stoppedAt: "2026-11-08T03:30:00Z",
    now: new Date("2026-11-08T03:42:06Z"),
    affected: ["網站與入口"],
    onRestart: vi.fn(),
    onViewLogs: vi.fn(),
  };

  it("names the component, host and how long it has been stopped, with exit code and raw reason", () => {
    render(
      <ComponentDownBanner {...base} exitCode={137} reason="OOMKilled" logTail={["line one", "line two"]} canRestart />,
    );
    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent("Zentinel 已停止 · hazel-tw-01 · 12 分鐘 6 秒");
    expect(alert).toHaveTextContent("結束碼 137");
    expect(alert).toHaveTextContent("OOMKilled");
    expect(alert).toHaveTextContent("網站與入口");
    expect(alert.querySelector("pre")!.textContent).toBe("line one\nline two");
    fireEvent.click(within(alert).getByRole("button", { name: /重啟 Zentinel/ }));
    expect(base.onRestart).toHaveBeenCalled();
  });

  it("keeps counting the stopped time when no reference time is passed", () => {
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval", "Date"] });
    try {
      vi.setSystemTime(new Date("2026-11-08T03:30:05Z"));
      const { now: _ignored, ...rest } = base;
      void _ignored;
      render(<ComponentDownBanner {...rest} canRestart />);
      expect(screen.getByRole("alert")).toHaveTextContent("5 秒");
      act(() => {
        vi.advanceTimersByTime(60_000);
      });
      expect(screen.getByRole("alert")).toHaveTextContent("1 分鐘 5 秒");
    } finally {
      vi.useRealTimers();
    }
  });

  it("keeps the restart button visible but disabled without permission", () => {
    render(<ComponentDownBanner {...base} canRestart={false} noPermissionContact="tim" />);
    expect(screen.getByRole("button", { name: /重啟 Zentinel/ })).toBeDisabled();
    expect(screen.getByRole("button", { name: /看日誌/ })).toBeEnabled();
  });
});

describe("NavCount", () => {
  const withStatus = (status: ShellStatus) =>
    render(
      <ShellStatusContext.Provider value={status}>
        <NavCount kind="attention" />
      </ShellStatusContext.Provider>,
    );

  it("shows nothing while the count is unknown or zero (no fake zero)", () => {
    expect(withStatus({}).container).toBeEmptyDOMElement();
    expect(withStatus({ attention: 0 }).container).toBeEmptyDOMElement();
  });

  it("shows the count with a spoken label", () => {
    withStatus({ attention: 5 });
    expect(screen.getByLabelText("需要處理 5 件")).toHaveTextContent("5");
  });
});
