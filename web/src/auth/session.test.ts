import { describe, expect, it } from "vite-plus/test";
import { ApiError } from "../api/client";
import { authKeys } from "./api";
import { createQueryClient } from "./session";

describe("createQueryClient", () => {
  it("forgets one account's cached data when its session is lost, keeping only the session query", async () => {
    const client = createQueryClient();
    client.setQueryData(authKeys.session, { user: { username: "alice" } });
    client.setQueryData(authKeys.factors, { passkeys: [{ name: "Alice's key" }] });
    client.setQueryData(authKeys.sessions, { sessions: [] });
    const lost = new ApiError({ status: 401, code: "UNAUTHENTICATED", message: "", raw: "" });
    await client
      .fetchQuery({ queryKey: ["me", "anything"], queryFn: () => Promise.reject(lost), retry: false })
      .catch(() => undefined);
    expect(client.getQueryData(authKeys.factors)).toBeUndefined();
    expect(client.getQueryData(authKeys.sessions)).toBeUndefined();
    expect(client.getQueryCache().find({ queryKey: authKeys.session })).toBeDefined();
  });

  it("keeps cached data on other errors", async () => {
    const client = createQueryClient();
    client.setQueryData(authKeys.factors, { passkeys: [] });
    const denied = new ApiError({ status: 403, code: "FORBIDDEN", message: "", raw: "" });
    await client
      .fetchQuery({ queryKey: ["me", "anything"], queryFn: () => Promise.reject(denied), retry: false })
      .catch(() => undefined);
    expect(client.getQueryData(authKeys.factors)).toEqual({ passkeys: [] });
  });
});
