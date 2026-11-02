import { describe, expect, it } from "vitest";
import { buildModel } from "@/lib/model";
import { SAMPLE_NOW, sampleAnswer } from "@/lib/sample";
import { facets, groups, href, readParams } from "./lib";

const m = buildModel(sampleAnswer("many"), SAMPLE_NOW);

describe("provider and project filters", () => {
  it("keeps only a provider some project is on", () => {
    expect(readParams({ provider: "vercel" }, m).provider).toBe("vercel");
    expect(readParams({ provider: "nope" }, m).provider).toBe("");
  });

  it("drops a project that isn't on the chosen provider", () => {
    expect(readParams({ provider: "vercel", project: "acme-dev" }, m).project).toBe("");
    expect(readParams({ provider: "supabase", project: "acme-dev" }, m).project).toBe("acme-dev");
    expect(readParams({ project: "acme-dev" }, m).project).toBe("acme-dev");
  });

  it("puts the provider in the link", () => {
    const p = readParams({ provider: "vercel", filter: "open" }, m);
    expect(href(p)).toBe("/resources?filter=open&provider=vercel");
    expect(href(p, { provider: "" })).toBe("/resources?filter=open");
  });

  it("lists only the chosen provider's projects and counts only its resources", () => {
    const f = facets(m, readParams({ provider: "vercel" }, m));
    expect(f.projects.map((x) => x.project.name)).toEqual(["acme-landing", "acme-web", "internal-tools"]);
    const vercelOpen = m.open.filter((r) => r.provider === "vercel").length;
    expect(f.status.open).toBe(vercelOpen);
    expect(f.status.open).toBeLessThan(m.open.length);
  });

  it("counts providers and projects within the chosen status", () => {
    const f = facets(m, readParams({ filter: "open" }, m));
    expect(f.providers.reduce((t, x) => t + x.count, 0)).toBe(m.open.length);
    expect(f.projects.find((x) => x.project.name === "acme-prod")?.count).toBe(0);
  });

  it("shows only the chosen provider's groups", () => {
    const gs = groups(m, readParams({ provider: "supabase" }, m), true);
    expect(gs.length).toBeGreaterThan(0);
    expect(gs.every((g) => g.project.provider === "supabase")).toBe(true);
  });
});
