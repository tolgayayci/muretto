import { HeaderSkeleton, RowsSkeleton, Sk } from "@/components/ui";

export default function Loading() {
  return (
    <div className="m-page m-sk-wrap" aria-busy="true" aria-label="Loading">
      <HeaderSkeleton />
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
      <div className="m-grid-3">
        {[0, 1, 2].map((i) => (
          <div key={i} className="m-card" style={{ height: 196, padding: 20, display: "flex", flexDirection: "column", gap: 16, boxSizing: "border-box" }}>
            <Sk w={160} h={14} />
            <Sk w="80%" h={14} />
            <Sk w="92%" h={11} dim />
          </div>
        ))}
      </div>
      <div className="m-grid-2">
        <RowsSkeleton />
        <RowsSkeleton />
      </div>
    </div>
  );
}
