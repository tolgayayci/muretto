import { Sk } from "@/components/ui";

const card = { display: "flex", flexDirection: "column", boxSizing: "border-box", border: "1px solid var(--line)", borderRadius: "var(--radius-2xl)", background: "var(--surface-raised)" } as const;

export default function Loading() {
  return (
    <div className="m-page m-sk-wrap" style={{ gap: 28 }} aria-busy="true" aria-label="Loading your providers">
      <header style={{ display: "flex", flexDirection: "column", gap: 14 }}>
        <Sk w={150} h={14} />
        <Sk w={220} h={34} />
        <Sk w={260} h={34} dim />
      </header>
      <div className="m-grid-2" style={{ gap: 20, alignItems: "stretch" }}>
        {[5, 4].map((cells, i) => (
          <div key={i} style={{ ...card, gap: 22, padding: 24 }}>
            <span style={{ display: "flex", alignItems: "center", gap: 14 }}>
              <Sk w={42} h={42} r={11} />
              <span style={{ display: "flex", flexDirection: "column", gap: 7, flexGrow: 1 }}>
                <Sk w={110} h={16} />
                <Sk w={160} h={10} dim />
              </span>
              <Sk w={96} h={26} r={999} />
            </span>
            <span style={{ display: "flex", flexDirection: "column", gap: 10 }}>
              <Sk w={100} h={10} dim />
              <span style={{ display: "grid", gridTemplateColumns: `repeat(${cells}, minmax(0, 1fr))`, height: 94, border: "1px solid var(--line)", borderRadius: "var(--radius-lg)", overflow: "hidden" }}>
                {Array.from({ length: cells }, (_, j) => (
                  <span key={j} style={{ display: "flex", flexDirection: "column", gap: 9, padding: 12, borderRight: j < cells - 1 ? "1px solid var(--line)" : undefined }}>
                    <Sk w="60%" h={10} />
                    <Sk w={28} h={20} />
                    <Sk w="75%" h={9} dim />
                  </span>
                ))}
              </span>
            </span>
            <span style={{ display: "flex", flexDirection: "column", gap: 10 }}>
              <Sk w={120} h={10} dim />
              <span style={{ display: "flex", flexDirection: "column", border: "1px solid var(--line)", borderRadius: "var(--radius-lg)", overflow: "hidden" }}>
                {[0, 1].map((j) => (
                  <span key={j} style={{ display: "flex", alignItems: "center", gap: 12, height: 56, padding: "0 14px", borderBottom: j === 0 ? "1px solid var(--line)" : undefined }}>
                    <Sk w={30} h={30} r={8} />
                    <span style={{ display: "flex", flexDirection: "column", gap: 7, flexGrow: 1 }}>
                      <Sk w="60%" h={11} />
                      <Sk w="40%" h={9} dim />
                    </span>
                  </span>
                ))}
              </span>
            </span>
          </div>
        ))}
      </div>
      <div className="m-grid-2" style={{ gap: 20, alignItems: "stretch" }}>
        {[0, 1].map((i) => (
          <div key={i} style={{ ...card, gap: 14, height: 236, padding: "22px 24px" }}>
            <Sk w={200} h={16} />
            <Sk w={280} h={11} dim />
            <span style={{ marginTop: 10 }}>
              <Sk w="100%" h={38} r={10} />
            </span>
            <Sk w="100%" h={38} r={10} />
          </div>
        ))}
      </div>
    </div>
  );
}
