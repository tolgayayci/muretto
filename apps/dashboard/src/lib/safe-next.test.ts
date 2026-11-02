import { describe, expect, it } from "vitest";
import { safeNext } from "./safe-next";

describe("safeNext", () => {
  it("keeps same-site paths", () => {
    expect(safeNext("/device?code=BKTW-HRZP")).toBe("/device?code=BKTW-HRZP");
    expect(safeNext("/resources")).toBe("/resources");
  });
  it("refuses anything that could leave the site", () => {
    for (const bad of ["https://evil.example", "//evil.example", "/\\evil.example", "evil", "/a\nb", "", null, undefined, "/auth/callback", "/login"]) {
      expect(safeNext(bad)).toBe("/");
    }
  });
});
