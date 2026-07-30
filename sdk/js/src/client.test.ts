import { describe, expect, it, vi } from "vitest";
import { NexusVPNClient } from "./client.js";

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("NexusVPNClient", () => {
  it("logs in and stores tokens", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      jsonResponse({ accessToken: "a1", refreshToken: "r1", expiresIn: 900 }),
    );
    const client = new NexusVPNClient({ baseUrl: "http://x/api/v1", fetchImpl: fetchMock as unknown as typeof fetch });
    const tokens = await client.login("a@example.com", "pw");
    expect(tokens.accessToken).toBe("a1");
    expect(fetchMock).toHaveBeenCalledWith(
      "http://x/api/v1/auth/login",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("refreshes the access token once on 401 and retries", async () => {
    let networksCalls = 0;
    const fetchMock = vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("/auth/refresh")) {
        return jsonResponse({ accessToken: "a2", refreshToken: "r2", expiresIn: 900 });
      }
      if (url.endsWith("/networks")) {
        networksCalls++;
        const auth = (init?.headers as Record<string, string>)?.Authorization;
        if (auth === "Bearer a2") return jsonResponse([{ id: "n1" }]);
        return new Response("unauthorized", { status: 401 });
      }
      throw new Error("unexpected url " + url);
    });

    const client = new NexusVPNClient({
      baseUrl: "http://x/api/v1",
      accessToken: "stale",
      refreshToken: "r1",
      fetchImpl: fetchMock as unknown as typeof fetch,
    });

    const networks = await client.listNetworks();
    expect(networks).toHaveLength(1);
    expect(networksCalls).toBe(2);
  });

  it("throws NexusVPNAPIError with code/message from the error envelope", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ error: { code: "not_found", message: "Network not found" } }), {
        status: 404,
        headers: { "Content-Type": "application/json" },
      }),
    );
    const client = new NexusVPNClient({ baseUrl: "http://x/api/v1", fetchImpl: fetchMock as unknown as typeof fetch });
    await expect(client.getNetwork("missing")).rejects.toMatchObject({
      code: "not_found",
      status: 404,
    });
  });
});
