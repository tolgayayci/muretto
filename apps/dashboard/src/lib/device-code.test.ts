import { describe, expect, it } from "vitest";
import { normalizeCode } from "./device-code";

describe("normalizeCode", () => {
  it("accepts the CLI's code in any common spelling", () => {
    expect(normalizeCode("BKTW-HRZP")).toBe("BKTW-HRZP");
    expect(normalizeCode("bktwhrzp")).toBe("BKTW-HRZP");
    expect(normalizeCode("BKTW HRZP")).toBe("BKTW-HRZP");
  });
  it("rejects anything else", () => {
    for (const bad of ["", "BKTW-HRZ", "BKTW-HRZPQ", "AEIO-UBCD", "BKTW-HRZ1", "<script>", null, undefined, "x".repeat(40)]) {
      expect(normalizeCode(bad)).toBeNull();
    }
  });
});
