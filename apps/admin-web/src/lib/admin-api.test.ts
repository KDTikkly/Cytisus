import { afterEach, describe, expect, it, vi } from "vitest";

import { adminAPI, AdminAPIError } from "./admin-api";

afterEach(() => vi.unstubAllGlobals());

describe("admin API", () => {
  it("sends an explicit synthetic actor identity and role", async () => {
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        void input;
        void init;
        return Response.json({ items: [] });
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    await adminAPI.cases({ id: "maker-a", role: "OPERATIONS" });

    const init = fetchMock.mock.calls[0]?.[1] as RequestInit;
    const headers = init.headers as Headers;
    expect(headers.get("X-Admin-ID")).toBe("maker-a");
    expect(headers.get("X-Admin-Role")).toBe("OPERATIONS");
  });

  it("preserves independent-approval guidance", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        Response.json(
          {
            error: {
              code: "INDEPENDENT_APPROVAL_REQUIRED",
              message: "The person who proposed this action cannot approve it.",
              next_action:
                "Ask a different authorized administrator to review the proposal.",
            },
          },
          { status: 409 },
        ),
      ),
    );

    await expect(
      adminAPI.decide({ id: "maker-a", role: "OPERATIONS" }, "proposal-id", {
        approve: true,
        decision_reason: "Reviewed evidence.",
      }),
    ).rejects.toEqual(
      new AdminAPIError(
        "INDEPENDENT_APPROVAL_REQUIRED",
        "The person who proposed this action cannot approve it.",
        "Ask a different authorized administrator to review the proposal.",
        409,
      ),
    );
  });
});
