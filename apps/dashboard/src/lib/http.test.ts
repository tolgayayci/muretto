import { describe, expect, it } from "vitest";
import { redirectHere, sameOrigin } from "./http";

const req = (h: Record<string, string>) => ({ headers: new Headers(h) });

describe("redirectHere", () => {
  it("sends a relative Location", () => {
    const r = redirectHere("/login?error=denied", 303);
    expect(r.status).toBe(303);
    expect(r.headers.get("location")).toBe("/login?error=denied");
  });
  it("refuses anything but a site path", () => {
    expect(() => redirectHere("https://evil.example")).toThrow();
    expect(() => redirectHere("//evil.example")).toThrow();
  });
});

describe("sameOrigin", () => {
  it("passes a post from the host the browser used", () => {
    expect(sameOrigin(req({ origin: "https://app.muretto.ai", host: "app.muretto.ai" }))).toBe(true);
    expect(sameOrigin(req({ origin: "http://localhost:3100", host: "localhost:3100" }))).toBe(true);
  });
  it("passes when there is no Origin", () => {
    expect(sameOrigin(req({ host: "app.muretto.ai" }))).toBe(true);
  });
  it("refuses another site or a bad Origin", () => {
    expect(sameOrigin(req({ origin: "https://evil.example", host: "app.muretto.ai" }))).toBe(false);
    expect(sameOrigin(req({ origin: "null", host: "app.muretto.ai" }))).toBe(false);
    expect(sameOrigin(req({ origin: "https://app.muretto.ai" }))).toBe(false);
  });
});
