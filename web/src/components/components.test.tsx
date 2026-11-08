import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { ReactNode } from "react";
import { createMemoryRouter, Link, MemoryRouter, RouterProvider } from "react-router";
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

/** SaveBar uses useBlocker, which needs a data router; "/other" is the page a test can navigate to. */
const inDataRouter = (ui: ReactNode) => {
  const router = createMemoryRouter(
    [
      {
        path: "/",
        element: (
          <>
            <Link to="/other">go elsewhere</Link>
            {ui}
          </>
        ),
      },
      { path: "/other", element: <p>other page</p> },
    ],
    { initialEntries: ["/"] },
  );
  const view = render(<RouterProvider router={router} />);
  return { ...view, router };
};

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
    expect(within(bar).getByRole("link", { name: "處理" })).toHaveAttribute("href", "/nodes/a");
    expect(within(bar).queryByRole("button", { name: /關閉|close/i })).toBeNull();
  });
});

describe("SaveBar", () => {
  const props = { effect: "存檔後要重建 shop-api 才會生效", saveLabel: "儲存並重建 shop-api" };

  it("is absent without unsaved changes", () => {
    inDataRouter(<SaveBar changes={0} {...props} onSave={vi.fn()} onDiscard={vi.fn()} />);
    expect(screen.queryByRole("region", { name: "未儲存的變更" })).toBeNull();
  });

  it("states the count and the effect, and wires discard and save", () => {
    const onSave = vi.fn();
    const onDiscard = vi.fn();
    inDataRouter(<SaveBar changes={3} {...props} onSave={onSave} onDiscard={onDiscard} />);
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
    const { unmount } = inDataRouter(<SaveBar changes={1} {...props} onSave={onSave} onDiscard={vi.fn()} />);
    const ctrl = new KeyboardEvent("keydown", { key: "s", ctrlKey: true, cancelable: true });
    window.dispatchEvent(ctrl);
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "s", metaKey: true, cancelable: true }));
    expect(onSave).toHaveBeenCalledTimes(2);
    expect(ctrl.defaultPrevented).toBe(true);
    unmount();
    inDataRouter(<SaveBar changes={1} {...props} saving onSave={onSave} onDiscard={vi.fn()} />);
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "s", ctrlKey: true, cancelable: true }));
    expect(onSave).toHaveBeenCalledTimes(2);
  });

  it("leaves Ctrl+S alone when there is nothing to save", () => {
    const onSave = vi.fn();
    inDataRouter(<SaveBar changes={0} {...props} onSave={onSave} onDiscard={vi.fn()} />);
    const ev = new KeyboardEvent("keydown", { key: "s", ctrlKey: true, cancelable: true });
    window.dispatchEvent(ev);
    expect(onSave).not.toHaveBeenCalled();
    expect(ev.defaultPrevented).toBe(false);
  });

  it("ignores auto-repeat and IME composition so holding ⌘S saves once", () => {
    const onSave = vi.fn();
    inDataRouter(<SaveBar changes={1} {...props} onSave={onSave} onDiscard={vi.fn()} />);
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "s", metaKey: true, cancelable: true }));
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "s", metaKey: true, repeat: true, cancelable: true }));
    window.dispatchEvent(
      new KeyboardEvent("keydown", { key: "s", metaKey: true, isComposing: true, cancelable: true }),
    );
    expect(onSave).toHaveBeenCalledTimes(1);
  });

  it("disables every button and reads 儲存中… while saving", () => {
    inDataRouter(<SaveBar changes={1} {...props} saving onSave={vi.fn()} onDiscard={vi.fn()} />);
    const bar = screen.getByRole("region", { name: "未儲存的變更" });
    expect(within(bar).getByRole("button", { name: "捨棄" })).toBeDisabled();
    expect(within(bar).getByRole("button", { name: /儲存中…/ })).toBeDisabled();
  });

  it("shows why the save failed and turns the primary into 再試一次", () => {
    const onSave = vi.fn();
    inDataRouter(
      <SaveBar
        changes={1}
        subject=".env"
        {...props}
        error="磁碟唯讀（PLATFORM_DEGRADED）"
        onSave={onSave}
        onDiscard={vi.fn()}
      />,
    );
    const bar = screen.getByRole("region", { name: "未儲存的變更" });
    expect(bar).toHaveTextContent(".env：1 處未儲存");
    expect(bar).toHaveTextContent("沒有存成功 · 磁碟唯讀（PLATFORM_DEGRADED）");
    fireEvent.click(within(bar).getByRole("button", { name: /再試一次/ }));
    expect(onSave).toHaveBeenCalledTimes(1);
  });

  it("asks before in-app navigation: 留在這頁 keeps the page", async () => {
    const { router } = inDataRouter(
      <SaveBar changes={1} subject=".env" {...props} onSave={vi.fn()} onDiscard={vi.fn()} />,
    );
    fireEvent.click(screen.getByRole("link", { name: "go elsewhere" }));
    const dialog = await screen.findByRole("dialog", { name: ".env 有 1 處沒存，要離開嗎？" });
    fireEvent.click(within(dialog).getByRole("button", { name: "留在這頁" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(router.state.location.pathname).toBe("/");
  });

  it("lets navigation through when guardNavigation is off", async () => {
    const { router } = inDataRouter(
      <SaveBar changes={1} {...props} guardNavigation={false} onSave={vi.fn()} onDiscard={vi.fn()} />,
    );
    fireEvent.click(screen.getByRole("link", { name: "go elsewhere" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/other"));
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("不存，離開 navigates without saving", async () => {
    const onSave = vi.fn();
    const { router } = inDataRouter(<SaveBar changes={1} {...props} onSave={onSave} onDiscard={vi.fn()} />);
    fireEvent.click(screen.getByRole("link", { name: "go elsewhere" }));
    fireEvent.click(await screen.findByRole("button", { name: "不存，離開" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/other"));
    expect(onSave).not.toHaveBeenCalled();
  });

  it("儲存後離開 waits for the save, and stays when the save fails", async () => {
    let finish!: () => void;
    const ok = vi.fn(() => new Promise<void>((r) => (finish = r)));
    const first = inDataRouter(<SaveBar changes={1} {...props} onSave={ok} onDiscard={vi.fn()} />);
    fireEvent.click(screen.getByRole("link", { name: "go elsewhere" }));
    fireEvent.click(await screen.findByRole("button", { name: "儲存後離開" }));
    expect(ok).toHaveBeenCalledTimes(1);
    expect(first.router.state.location.pathname).toBe("/");
    await act(async () => finish());
    await waitFor(() => expect(first.router.state.location.pathname).toBe("/other"));
    first.unmount();

    const failing = vi.fn(() => Promise.reject(new Error("disk read-only")));
    const second = inDataRouter(<SaveBar changes={1} {...props} onSave={failing} onDiscard={vi.fn()} />);
    fireEvent.click(screen.getByRole("link", { name: "go elsewhere" }));
    fireEvent.click(await screen.findByRole("button", { name: "儲存後離開" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(second.router.state.location.pathname).toBe("/");
  });

  it("asks before the page unloads while changes are unsaved", () => {
    inDataRouter(<SaveBar changes={2} {...props} onSave={vi.fn()} onDiscard={vi.fn()} />);
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
    expect(screen.getByText("sshd 設定檢查沒有通過").closest(".hx-status-pill")).not.toBeNull();
    const details = container.querySelector("details")!;
    expect(details.open).toBe(false);
    expect(details.querySelector("summary")).toHaveTextContent("看原文（sshd -t）");
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
    expect(container.querySelector("summary")).toHaveTextContent("看原文（HazelminX Core · trace 7f3a）");
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

  it("disables the restart button and reads 重啟中… while restarting", () => {
    render(<ComponentDownBanner {...base} canRestart restarting />);
    expect(screen.getByRole("button", { name: "重啟中…" })).toBeDisabled();
  });

  it("states repeated failures and that the platform will not retry on its own", () => {
    render(<ComponentDownBanner {...base} canRestart failures={{ count: 2, lastError: "exit status 1" }} />);
    expect(screen.getByRole("alert")).toHaveTextContent("重啟 2 次都失敗 · 平台不會自動再試");
    expect(screen.getByRole("alert")).toHaveTextContent("exit status 1");
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
