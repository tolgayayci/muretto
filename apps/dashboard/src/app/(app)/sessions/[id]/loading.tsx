import { Sk } from "@/components/ui";

export default function Loading() {
  return (
    <div className="m-page m-sk-wrap" style={{ gap: 32 }} aria-busy="true" aria-label="Loading this session">
      <Sk w={140} h={13} />
      <header style={{ display: "flex", flexDirection: "column", gap: 22 }}>
        <div style={{ display: "flex", alignItems: "flex-start", gap: 20 }}>
          <Sk w={56} h={56} r={999} />
          <span style={{ display: "flex", flexDirection: "column", gap: 12, flexGrow: 1 }}>
            <Sk w={170} h={13} />
            <Sk w={460} h={32} />
            <Sk w={320} h={32} dim />
          </span>
        </div>
        <div style={{ display: "flex", flexWrap: "wrap", gap: 8 }}>
          {[150, 110, 160, 210].map((w) => (
            <Sk key={w} w={w} h={34} r={999} />
          ))}
        </div>
      </header>
      <div className="m-card m-strip">
        {[0, 1, 2, 3].map((i) => (
          <div key={i} className="m-strip-cell">
            <Sk w={30} h={30} />
            <span style={{ display: "flex", flexDirection: "column", gap: 7, flexGrow: 1 }}>
              <Sk w={110} h={12} />
              <Sk w="70%" h={10} dim />
            </span>
          </div>
        ))}
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
        <Sk w={150} h={18} />
        <div className="m-grid-3">
          {[0, 1, 2].map((i) => (
            <div key={i} className="m-card" style={{ height: 300, padding: 20, display: "flex", flexDirection: "column", gap: 16, boxSizing: "border-box" }}>
              <span style={{ display: "flex", alignItems: "center", gap: 12 }}>
                <Sk w={36} h={36} r={10} />
                <span style={{ display: "flex", flexDirection: "column", gap: 8, flexGrow: 1 }}>
                  <Sk w="60%" h={13} />
                  <Sk w="80%" h={10} dim />
                </span>
              </span>
              <Sk w="80%" h={14} />
              <Sk w="92%" h={11} dim />
              <span style={{ marginTop: "auto" }}>
                <Sk w="100%" h={96} r={12} dim />
              </span>
            </div>
          ))}
        </div>
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
        <Sk w={140} h={18} />
        <div className="m-list">
          {[0, 1, 2, 3].map((i) => (
            <div key={i} className="m-row">
              <Sk w={32} h={32} r={8} />
              <span style={{ display: "flex", flexDirection: "column", gap: 7, flexGrow: 1 }}>
                <Sk w="35%" h={12} />
                <Sk w="22%" h={10} dim />
              </span>
              <Sk w={110} h={22} r={999} />
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
