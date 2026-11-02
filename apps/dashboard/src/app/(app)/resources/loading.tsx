import { HeaderSkeleton, Sk } from "@/components/ui";
import s from "./_components/resources.module.css";

const CHIPS = [52, 128, 124, 156, 118, 62];

export default function Loading() {
  return (
    <div className="m-page m-sk-wrap" aria-busy="true" aria-label="Loading your resources" style={{ gap: 28 }}>
      <HeaderSkeleton />
      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
        {CHIPS.map((w) => (
          <Sk key={w} w={w} h={32} r={999} />
        ))}
        <span style={{ marginLeft: "auto" }}>
          <Sk w={220} h={32} r={999} dim />
        </span>
      </div>
      {[5, 2].map((rows, gi) => (
        <div key={gi} className={s.box}>
          <div className={s.groupHead}>
            <Sk w={80} h={14} />
            <Sk w={130} h={14} />
            <Sk w={90} h={11} dim />
          </div>
          <div>
            {Array.from({ length: rows }, (_, i) => (
              <div key={i} className={s.row} style={{ height: 72 }}>
                <Sk w={36} h={36} r={10} />
                <span style={{ display: "flex", flexDirection: "column", gap: 7 }}>
                  <Sk w="60%" h={12} />
                  <Sk w="40%" h={10} dim />
                </span>
                <span style={{ display: "flex", alignItems: "center", gap: 10 }}>
                  <Sk w={26} h={26} r={999} />
                  <Sk w="70%" h={11} dim />
                </span>
                <Sk w={96} h={20} dim />
                <span style={{ justifySelf: "end" }}>
                  <Sk w={48} h={11} dim />
                </span>
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}
