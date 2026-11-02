import { describe, expect, it } from "vitest";
import { providerName } from "@/components/brand";
import { buildModel } from "@/lib/model";
import { SAMPLE_NOW, sampleAnswer } from "@/lib/sample";
import { FLAGS, checkProblems, flagResult, groupTone, kindCells, kindLabel } from "./derive";

describe("providers page", () => {
  it("labels kinds in plain words", () => {
    expect(kindLabel("vercel.env_var")).toBe("env vars");
    expect(kindLabel("supabase.branch")).toBe("branches");
  });

  it("tells account domains from project domains", () => {
    expect(kindLabel("vercel.domain")).toBe("domains");
    expect(kindLabel("vercel.project_domain")).toBe("project domains");
  });

  it("shows every check fresh in the normal scenario", () => {
    const m = buildModel(sampleAnswer("normal"), SAMPLE_NOW);
    const sb = m.checks.find((g) => g.provider === "supabase")!;
    expect(groupTone(sb)).toBe("ok");
    expect(kindCells(sb, m.now).map((c) => c.label)).toEqual(["projects", "branches", "tables", "buckets", "functions"]);
    expect(kindCells(sb, m.now).every((c) => c.state === "ok" && c.note === "3 min ago")).toBe(true);
    expect(flagResult(m, sb, FLAGS.supabase[0])).toEqual({ kind: "open", count: 1 });
    expect(checkProblems(m, providerName)).toEqual([]);
  });

  it("says can't tell, never none, when the token was rejected", () => {
    const m = buildModel(sampleAnswer("problem"), SAMPLE_NOW);
    const vc = m.checks.find((g) => g.provider === "vercel")!;
    expect(groupTone(vc)).toBe("down");
    expect(kindCells(vc, m.now)[0]).toMatchObject({ state: "old", note: "2 days old" });
    expect(FLAGS.vercel.map((f) => flagResult(m, vc, f).kind)).toEqual(["unknown", "unknown"]);
    const [p] = checkProblems(m, providerName);
    expect(p).toMatchObject({ title: "Vercel rejected your token", command: "muretto init", checks: 4 });
  });

  it("explains permission and rate limit failures", () => {
    const a = sampleAnswer("normal");
    a.checks = a.checks.map((c) =>
      c.kind === "supabase.bucket"
        ? { ...c, status: "error", error_code: "permission", last_success_at: undefined }
        : c.kind === "supabase.function"
          ? { ...c, status: "error", error_code: "rate_limit" }
          : c,
    );
    const m = buildModel(a, SAMPLE_NOW);
    const sb = m.checks.find((g) => g.provider === "supabase")!;
    expect(groupTone(sb)).toBe("partial");
    const cells = kindCells(sb, m.now);
    expect(cells.find((c) => c.label === "buckets")).toMatchObject({ state: "denied", note: "no access", items: undefined });
    expect(cells.find((c) => c.label === "functions")).toMatchObject({ state: "retry" });
    const ps = checkProblems(m, providerName);
    expect(ps.map((p) => p.title)).toEqual(["Supabase buckets can't be checked", "Supabase functions: too many requests"]);
    expect(ps[1].retrying).toBe(true);
    expect(ps[1].command).toBeUndefined();
  });
});
