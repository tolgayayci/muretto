"use client";

// A filter dropdown for list toolbars: a trigger that shows the chosen value,
// and a menu of links (each option is a URL, so the server renders the result).
import Link from "next/link";
import { Fragment, useEffect, useRef, useState, type ReactNode } from "react";
import s from "./facet.module.css";

export interface FacetOption {
  key: string;
  href: string;
  label: ReactNode;
  /** Leading mark: a provider logo, a red dot. */
  mark?: ReactNode;
  count?: number;
  red?: boolean;
  selected: boolean;
  /** Section heading this option sits under; a new value starts a section. */
  group?: ReactNode;
  groupKey?: string;
}

export function Facet({ label, value, mark, options, align = "start", active }: { label: string; value: ReactNode; mark?: ReactNode; options: FacetOption[]; align?: "start" | "end"; active: boolean }) {
  const [open, setOpen] = useState(false);
  const [end, setEnd] = useState(align === "end");
  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const marks = options.some((o) => o.mark);

  useEffect(() => {
    if (!open) return;
    const items = () => Array.from(menu.current?.querySelectorAll<HTMLElement>("[role=menuitemradio]") ?? []);
    (items().find((x) => x.getAttribute("aria-checked") === "true") ?? items()[0])?.focus();
    const onDown = (e: MouseEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setOpen(false);
        trigger.current?.focus();
        return;
      }
      if (e.key === "Tab") return setOpen(false);
      if (e.key !== "ArrowDown" && e.key !== "ArrowUp" && e.key !== "Home" && e.key !== "End") return;
      e.preventDefault();
      const all = items();
      const i = all.indexOf(document.activeElement as HTMLElement);
      const next = e.key === "Home" ? 0 : e.key === "End" ? all.length - 1 : (i + (e.key === "ArrowDown" ? 1 : -1) + all.length) % all.length;
      all[next]?.focus();
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  function toggle() {
    // Open toward the left when a 240px menu wouldn't fit to the right.
    const r = trigger.current?.getBoundingClientRect();
    setEnd(align === "end" || (!!r && r.left + 260 > window.innerWidth));
    setOpen((v) => !v);
  }

  return (
    <div ref={root} className={s.root}>
      <button ref={trigger} type="button" aria-haspopup="menu" aria-expanded={open} onClick={toggle} className={`${s.trigger}${active ? ` ${s.active}` : ""}`}>
        <span className={s.label}>{label}</span>
        {active ? (
          <span className={s.value}>
            {mark}
            {value}
          </span>
        ) : null}
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden className={s.chev}>
          <path d="m6 9 6 6 6-6" />
        </svg>
      </button>
      {open ? (
        <div ref={menu} role="menu" aria-label={label} className={`${s.menu}${end ? ` ${s.end}` : ""}`}>
          {options.map((o, i) => (
            <Fragment key={o.key}>
              {o.group && o.groupKey !== options[i - 1]?.groupKey ? (
                <span className={s.group} role="presentation">
                  {o.group}
                </span>
              ) : null}
              <Link role="menuitemradio" aria-checked={o.selected} href={o.href} onClick={() => setOpen(false)} className={`${s.item}${o.count === 0 && !o.selected ? ` ${s.dim}` : ""}`}>
                <span className={s.check} aria-hidden>
                  {o.selected ? "✓" : ""}
                </span>
                {marks ? <span className={s.mark}>{o.mark}</span> : null}
                <span className={s.itemLabel}>{o.label}</span>
                {o.count !== undefined ? <span className={`${s.count}${o.red && o.count > 0 ? ` ${s.red}` : ""}`}>{o.count}</span> : null}
              </Link>
            </Fragment>
          ))}
        </div>
      ) : null}
    </div>
  );
}
