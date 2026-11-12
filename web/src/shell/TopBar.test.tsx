import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { beforeAll, describe, expect, it } from "vite-plus/test";
import { ShellStatusContext, type ShellStatus } from "../app/shellStatus";
import { initI18n } from "../i18n";
import { TopBar } from "./TopBar";

beforeAll(() => {
  initI18n("zh-TW");
});

const topBar = (status: ShellStatus) =>
  render(
    <QueryClientProvider client={new QueryClient()}>
      <MemoryRouter>
        <ShellStatusContext.Provider value={status}>
          <TopBar />
        </ShellStatusContext.Provider>
      </MemoryRouter>
    </QueryClientProvider>,
  );

describe("TopBar", () => {
  it("signed out: only the brand, theme and language", () => {
    topBar({ signedOut: true });
    expect(screen.getByRole("link", { name: "HazelminX 總覽" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "切換成淺色" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "語言" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "搜尋或下指令" })).toBeNull();
    expect(screen.queryByRole("link", { name: /事件與告警/ })).toBeNull();
    expect(screen.queryByRole("link", { name: "終端機" })).toBeNull();
    expect(screen.queryByRole("button", { name: "帳號" })).toBeNull();
  });

  it("shows 觀測 — before the first observation and no jobs button", () => {
    topBar({});
    expect(screen.getByText("—")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /進行中的工作|工作中心/ })).toBeNull();
  });

  it("names the signed-in user on the account button", () => {
    topBar({ user: { name: "tim", role: "擁有者" } });
    expect(screen.getByRole("button", { name: "tim · 擁有者" })).toHaveTextContent("tim");
  });

  it("warns when the observation is older than 40 seconds", () => {
    topBar({ observedAt: new Date(Date.now() - 60_000).toISOString(), refreshSeconds: 10 });
    expect(screen.getByText("超過 40 秒沒更新")).toBeInTheDocument();
  });

  it("does not warn for a fresh observation", () => {
    topBar({ observedAt: new Date(Date.now() - 5_000).toISOString(), refreshSeconds: 10 });
    expect(screen.queryByText("超過 40 秒沒更新")).toBeNull();
  });
});
