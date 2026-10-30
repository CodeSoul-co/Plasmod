export class PlasmodError extends Error {
  constructor(message, { status = 0, code = "request_failed", detail, effectUnknown = false, cause } = {}) {
    super(message, { cause });
    Object.assign(this, { name: "PlasmodError", status, code, detail, effectUnknown });
  }
}

/** v1 HTTP client. Writes are never automatically retried. */
export class PlasmodClient {
  constructor(baseUrl = "http://127.0.0.1:8080", { headers = {}, fetch: fetchImpl } = {}) {
    this.baseUrl = baseUrl.replace(/\/$/, "");
    this.headers = headers;
    this.fetch = fetchImpl;
  }

  async request(path, options = {}) {
    const method = options.method || "GET";
    let response;
    try {
      response = await (this.fetch || globalThis.fetch)(`${this.baseUrl}${path}`, {
        ...options, headers: { ...this.headers, ...options.headers },
      });
    } catch (cause) {
      throw new PlasmodError("Plasmod request outcome is unknown", {
        code: "transport_error", effectUnknown: method !== "GET", cause,
      });
    }
    if (!response.ok) {
      const detail = await response.text();
      throw new PlasmodError(`Plasmod request failed (${response.status}): ${detail}`, {
        status: response.status, code: `http_${response.status}`, detail,
        effectUnknown: method !== "GET" && response.status >= 500,
      });
    }
    if (response.status === 204) return null;
    try { return await response.json(); }
    catch (cause) {
      throw new PlasmodError("Invalid Plasmod response", { code: "invalid_response", effectUnknown: method !== "GET", cause });
    }
  }

  post(path, body, options = {}) {
    return this.request(path, { ...options, method: "POST", headers: { "content-type": "application/json", ...options.headers }, body: JSON.stringify(body) });
  }
  get(path, params = {}, options = {}) {
    const query = new URLSearchParams(Object.entries(params).filter(([, v]) => v !== undefined && v !== null));
    return this.request(`${path}${query.size ? `?${query}` : ""}`, { ...options, method: "GET" });
  }
  getConsistencyMode() { return this.get("/v1/admin/consistency-mode"); }
  setConsistencyMode(mode) { return this.post("/v1/admin/consistency-mode", { mode }); }
  ingestEvent(event, options) { return this.post("/v1/ingest/events", event, options); }
  ingestDocument(document, options) { return this.post("/v1/ingest/document", document, options); }
  query(query, options) { return this.post("/v1/query", query, options); }
  recall(query, options) { return this.post("/v1/internal/memory/recall", query, options); }
  listMemories(scope, options) { return this.get("/v1/memory", scope, options); }
  listStates(scope, options) { return this.get("/v1/states", scope, options); }
  evidence(id, scope, options) { return this.get(`/v1/traces/${encodeURIComponent(id)}`, scope, options); }
  maintain(request, options) { return this.post("/v1/internal/memory/maintenance", request, options); }
  maintenanceStatus(id, scope, options) { return this.get(`/v1/internal/memory/operations/${encodeURIComponent(id)}`, scope, options); }
  mutate(request, options) { return this.post("/v1/internal/memory/mutate", request, options); }
  forget(request, options) { return this.mutate({ ...request, operation: "forget" }, options); }
  correct(request, options) { return this.mutate({ ...request, operation: "correct" }, options); }
  mutationStatus(id, scope, options) { return this.get(`/v1/internal/memory/mutations/${encodeURIComponent(id)}`, scope, options); }
}

/** Backwards compatible name from SDK 0.1. */
export class AndbClient extends PlasmodClient {}
