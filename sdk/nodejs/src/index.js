export class AndbClient {
  constructor(baseUrl = "http://127.0.0.1:8080") {
    this.baseUrl = baseUrl.replace(/\/$/, "");
  }

  async request(path, options = {}) {
    const response = await fetch(`${this.baseUrl}${path}`, options);
    if (!response.ok) {
      const detail = await response.text();
      throw new Error(`Plasmod request failed (${response.status}): ${detail}`);
    }
    return response.json();
  }

  async getConsistencyMode() {
    return this.request("/v1/admin/consistency-mode", { method: "GET" });
  }

  async setConsistencyMode(mode) {
    return this.request("/v1/admin/consistency-mode", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ mode }),
    });
  }
}
