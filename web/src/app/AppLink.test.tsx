import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { describe, expect, it } from "vite-plus/test";
import { AppLink } from "./AppLink";

const link = (href: string) => {
  render(
    <MemoryRouter>
      <AppLink href={href}>go</AppLink>
    </MemoryRouter>,
  );
  return screen.getByText("go");
};

describe("AppLink", () => {
  it("routes in-app paths through the router", () => {
    expect(link("/nodes/a%2Fb")).toHaveAttribute("href", "/nodes/a%2Fb");
  });
  it.each(["https://example.com/x", "mailto:ops@example.com", "#main"])("keeps %s as a plain link", (href) => {
    expect(link(href)).toHaveAttribute("href", href);
  });
  it.each(["javascript:alert(1)", "JaVaScRiPt:alert(1)", "data:text/html,<script>1</script>", "vbscript:x"])(
    "drops the href for %s",
    (href) => {
      expect(link(href)).not.toHaveAttribute("href");
    },
  );
});
