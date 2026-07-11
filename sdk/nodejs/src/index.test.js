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
