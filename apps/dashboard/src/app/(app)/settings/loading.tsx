import { HeaderSkeleton, Sk } from "@/components/ui";

const card = { padding: 24, display: "flex", flexDirection: "column", gap: 18, borderRadius: "var(--radius-2xl)" } as const;

function Lines({ n }: { n: number }) {
  return (
    <span style={{ display: "flex", flexDirection: "column" }}>
      {Array.from({ length: n }, (_, i) => (
        <span key={i} style={{ display: "flex", justifyContent: "space-between", alignItems: "center", height: 40, borderTop: "1px solid var(--line)" }}>
          <Sk w={90} h={11} dim />
          <Sk w={70} h={11} />
        </span>
      ))}
    </span>
  );
}

export default function Loading() {
  return (
    <div className="m-page m-sk-wrap" style={{ gap: 32 }} aria-busy="true" aria-label="Loading your settings">
      <HeaderSkeleton />
      <div className="m-grid-2" style={{ gap: 20 }}>
        {[0, 1].map((i) => (
          <div key={i} className="m-card" style={{ ...card, height: 290 }}>
            <span style={{ display: "flex", justifyContent: "space-between" }}>
              <Sk w={90} h={16} />
              <Sk w={60} h={22} r={999} />
            </span>
            <span style={{ display: "flex", alignItems: "center", gap: 14 }}>
              <Sk w={48} h={48} r={999} />
              <span style={{ display: "flex", flexDirection: "column", gap: 8 }}>
                <Sk w={140} h={15} />
                <Sk w={100} h={11} dim />
              </span>
            </span>
            <Lines n={2} />
            <span style={{ marginTop: "auto" }}>
              <Sk w="100%" h={36} r={8} dim />
            </span>
          </div>
        ))}
      </div>
      <div className="m-section">
        <Sk w={160} h={20} />
        <div className="m-grid-2" style={{ gap: 20 }}>
          {[0, 1].map((i) => (
            <div key={i} className="m-card" style={{ ...card, height: 240, gap: 16 }}>
              <span style={{ display: "flex", alignItems: "center", gap: 12 }}>
                <Sk w={36} h={36} r={10} />
                <span style={{ display: "flex", flexDirection: "column", gap: 7, flexGrow: 1 }}>
                  <Sk w={150} h={14} />
                  <Sk w={110} h={10} dim />
                </span>
                <Sk w={86} h={26} r={999} />
              </span>
              <Lines n={2} />
              <span style={{ marginTop: "auto" }}>
                <Sk w="100%" h={36} r={8} dim />
              </span>
            </div>
          ))}
        </div>
      </div>
      <div className="m-section">
        <Sk w={240} h={20} />
        <div className="m-grid-2" style={{ gap: 20 }}>
          {[0, 1].map((i) => (
            <div key={i} className="m-card" style={{ ...card, height: 400, gap: 16 }}>
              <span style={{ display: "flex", alignItems: "center", gap: 10 }}>
                <Sk w={32} h={32} r={9} />
                <Sk w={160} h={16} />
              </span>
              {Array.from({ length: 7 }, (_, k) => (
                <span key={k} style={{ margin: "5px 0" }}>
                  <Sk w="78%" h={12} dim />
                </span>
              ))}
              <span style={{ marginTop: "auto" }}>
                <Sk w="100%" h={34} r={8} dim />
              </span>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}
