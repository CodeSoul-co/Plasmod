import assert from "node:assert/strict";
import test from "node:test";

import { AndbClient } from "./index.js";

test("getConsistencyMode uses the admin endpoint", async () => {
  const calls = [];
  globalThis.fetch = async (url, options) => {
    calls.push([url, options]);
    return {
      ok: true,
      json: async () => ({ status: "ok", mode: "strict_visible" }),
    };
  };

  const client = new AndbClient("http://plasmod.test");
  const result = await client.getConsistencyMode();

  assert.equal(result.mode, "strict_visible");
  assert.equal(calls[0][0], "http://plasmod.test/v1/admin/consistency-mode");
  assert.equal(calls[0][1].method, "GET");
});

test("setConsistencyMode posts the requested mode", async () => {
  const calls = [];
  globalThis.fetch = async (url, options) => {
    calls.push([url, options]);
    return {
      ok: true,
      json: async () => ({ status: "ok", mode: "eventual_visibility" }),
    };
  };

  const client = new AndbClient("http://plasmod.test/");
  const result = await client.setConsistencyMode("eventual_visibility");

  assert.equal(result.mode, "eventual_visibility");
  assert.equal(calls[0][0], "http://plasmod.test/v1/admin/consistency-mode");
  assert.equal(calls[0][1].method, "POST");
  assert.deepEqual(JSON.parse(calls[0][1].body), {
    mode: "eventual_visibility",
  });
});

import { PlasmodClient, PlasmodError } from "./index.js";

test("maintenance passes scope, idempotency and cancellation without retry", async () => {
 const calls = [];
 const controller = new AbortController();
 const client = new PlasmodClient("http://test", { headers: { authorization: "Bearer token" }, fetch: async (url, opts) => { calls.push([url, opts]); throw new Error("connection lost"); } });
 const request = { operation_id: "op", operation: "update", tenant_id: "tenant", workspace_id: "ws", memory_ids: ["m"] };
 await assert.rejects(client.maintain(request, { signal: controller.signal }), (e) => e instanceof PlasmodError && e.effectUnknown && e.code === "transport_error");
 assert.equal(calls.length, 1);
 assert.deepEqual(JSON.parse(calls[0][1].body), request);
 assert.equal(calls[0][1].signal, controller.signal);
 assert.equal(calls[0][1].headers.authorization, "Bearer token");
});

test("mutation status scopes and encodes the operation ID", async () => {
 let requested;
 const client = new PlasmodClient("http://test", { fetch: async (url) => { requested = url; return { ok: true, json: async () => ({ status: "partial" }) }; } });
 assert.equal((await client.mutationStatus("a/b", { tenant_id: "t", workspace_id: "w" })).status, "partial");
 assert.equal(requested, "http://test/v1/internal/memory/mutations/a%2Fb?tenant_id=t&workspace_id=w");
});
