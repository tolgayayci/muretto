import { Sk } from "@/components/ui";
import s from "../_components/resources.module.css";

const card = { border: "1px solid var(--line)", borderRadius: "var(--radius-2xl)", background: "var(--surface-raised)", padding: 26, display: "flex", flexDirection: "column", gap: 16 } as const;

export default function Loading() {
  return (
    <div className="m-page m-sk-wrap" aria-busy="true" aria-label="Loading this resource" style={{ gap: 32, marginTop: -8 }}>
      <Sk w={200} h={13} dim />
      <header style={{ display: "flex", flexDirection: "column", gap: 14 }}>
        <Sk w={150} h={14} />
        <Sk w={420} h={34} />
        <Sk w={340} h={34} dim />
      </header>
      <div className={s.detail}>
        <div style={{ display: "flex", flexDirection: "column", gap: 24 }}>
          <div style={{ ...card, height: 320, boxSizing: "border-box" }}>
            <Sk w="70%" h={16} />
            <Sk w="95%" h={12} dim />
            <Sk w="80%" h={12} dim />
            <Sk w="100%" h={48} r={10} />
          </div>
          <div style={{ ...card, height: 220, boxSizing: "border-box" }}>
            <Sk w={140} h={16} />
            <span style={{ display: "flex", alignItems: "center", gap: 12 }}>
              <Sk w={34} h={34} r={999} />
              <Sk w="60%" h={12} />
            </span>
            <Sk w="100%" h={42} r={10} dim />
          </div>
        </div>
        <div style={{ display: "flex", flexDirection: "column", gap: 24 }}>
          <div style={{ ...card, padding: 22, height: 160, boxSizing: "border-box" }}>
            <Sk w={120} h={15} />
            <Sk w="100%" h={40} r={10} dim />
          </div>
          <div style={{ ...card, padding: 22, height: 300, boxSizing: "border-box" }}>
            <Sk w={80} h={15} />
            {[0, 1, 2, 3, 4, 5].map((i) => (
              <Sk key={i} w="100%" h={12} dim />
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
