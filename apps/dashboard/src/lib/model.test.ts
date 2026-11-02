import { describe, expect, it } from "vitest";
import { buildModel, problems, sessionById, sessionResources, shortKind } from "./model";
import { SAMPLE_NOW, sampleAnswer } from "./sample";

describe("buildModel: the main story", () => {
  const m = buildModel(sampleAnswer("normal"), SAMPLE_NOW);

  it("finds the three things open to anyone, worst first", () => {
    expect(m.state).toBe("attention");
    expect(m.open.map((r) => r.name)).toEqual(["customers", "customer-files", "preview-a81f"]);
    expect(m.open[0].open[0].what).toBe("Anyone can read every row.");
  });

  it("counts what is waiting on you, most overdue first", () => {
    expect(m.waiting.map((r) => [r.name, r.review?.overdueDays])).toEqual([
      ["acme-dev", 4],
      ["preview-77aa", 2],
      ["events", 1],
    ]);
    expect(m.comingUp.length).toBe(5);
    expect(m.decided.map((r) => r.name)).toEqual(["send-welcome"]);
  });

  it("separates agent-made, origin unknown and gone", () => {
    expect(m.agentMade.length).toBe(9);
    expect(m.unknownOrigin.map((r) => r.name).sort()).toEqual(["acme-prod", "acme-web", "legacy_users"]);
    expect(m.gone.map((r) => r.name)).toEqual(["preview-1c0e"]);
  });

  it("groups resources by project", () => {
    const dev = m.projects.find((p) => p.name === "acme-dev");
    expect(dev?.provider).toBe("supabase");
    expect(dev?.open).toBe(2);
    expect(m.resources.find((r) => r.name === "preview-a81f")?.project).toBe("acme-web");
  });

  it("links sessions to what they made", () => {
    const s = sessionById(m, "8f2a");
    expect(s?.title).toBe("add customer import with file uploads");
    expect(s?.openCount).toBe(3);
    expect(sessionResources(m, s!).length).toBe(5);
    expect(sessionById(m, "7a0c")?.transcriptOnly).toBe(true);
  });

  it("reports when providers were last checked", () => {
    expect(m.checkedAt?.toISOString()).toBe("2026-11-01T14:37:00.000Z");
    expect(problems(m)).toEqual([]);
  });
});

describe("buildModel: other states", () => {
  it("new user", () => {
    const m = buildModel(sampleAnswer("new"), SAMPLE_NOW);
    expect(m.state).toBe("new");
    expect(m.hasProviders).toBe(false);
  });

  it("connected, no sessions yet", () => {
    const m = buildModel(sampleAnswer("waiting"), SAMPLE_NOW);
    expect(m.state).toBe("waiting");
    expect(m.unknownOrigin.length).toBe(14);
    expect(m.open.length).toBe(0);
  });

  it("all clear", () => {
    const m = buildModel(sampleAnswer("clear"), SAMPLE_NOW);
    expect(m.state).toBe("clear");
    expect(m.waiting.map((r) => r.name)).toEqual(["preview-77aa"]);
  });

  it("many projects", () => {
    const m = buildModel(sampleAnswer("many"), SAMPLE_NOW);
    expect(m.open.length).toBe(7);
    expect(new Set(m.open.map((r) => r.project)).size).toBe(4);
  });

  it("problems: a failed check makes findings can't tell", () => {
    const m = buildModel(sampleAnswer("problem"), SAMPLE_NOW);
    expect(m.open.map((r) => r.name)).toEqual(["customers", "customer-files"]);
    expect(m.cantTell.map((r) => r.name)).toEqual(["preview-77aa", "preview-a81f"]);
    const p = problems(m);
    expect(p[0].title).toBe("Vercel rejected your token");
    expect(p.some((x) => x.code === "hooks_not_installed")).toBe(true);
  });
});

describe("shortKind", () => {
  it("names kinds plainly", () => {
    expect(shortKind({ kind: "vercel.deployment", attributes: { target: "preview" } })).toBe("preview");
    expect(shortKind({ kind: "vercel.deployment", attributes: { target: "production" } })).toBe("deployment");
    expect(shortKind({ kind: "vercel.env_var", attributes: {} })).toBe("env var");
    expect(shortKind({ kind: "supabase.table", attributes: {} })).toBe("table");
  });
});
