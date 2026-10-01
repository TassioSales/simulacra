import { useEffect, useRef, useState, type RefObject } from "react";
import type { TimeWindow } from "./useTimeWindow";

/** Measures an element's width and wires wheel-zoom and drag-pan to a TimeWindow. */
export function usePanZoom(ref: RefObject<HTMLElement | null>, win: TimeWindow, gutter: number) {
  const [width, setWidth] = useState(900);
  const winRef = useRef(win);
  winRef.current = win;

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(([e]) => setWidth(Math.max(320, Math.floor(e!.contentRect.width))));
    ro.observe(el);

    const timeAt = (clientX: number) => {
      const r = el.getBoundingClientRect();
      const w = winRef.current;
      return w.t0 + ((clientX - r.left - gutter) / (r.width - gutter)) * w.span;
    };
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const w = winRef.current;
      if (e.shiftKey || Math.abs(e.deltaX) > Math.abs(e.deltaY)) {
        const d = e.shiftKey ? e.deltaY : e.deltaX;
        w.pan((d / el.clientWidth) * w.span);
      } else {
        w.zoom(e.deltaY > 0 ? 1.25 : 0.8, timeAt(e.clientX));
      }
    };
    let dragX: number | null = null;
    const onDown = (e: PointerEvent) => {
      if (e.button !== 0) return;
      dragX = e.clientX;
    };
    const onMove = (e: PointerEvent) => {
      if (dragX === null) return;
      const dx = e.clientX - dragX;
      if (Math.abs(dx) < 2) return;
      const w = winRef.current;
      w.pan((-dx / (el.clientWidth - gutter)) * w.span);
      dragX = e.clientX;
      el.style.cursor = "grabbing";
    };
    const onUp = () => {
      dragX = null;
      el.style.cursor = "";
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    el.addEventListener("pointerdown", onDown);
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    return () => {
      ro.disconnect();
      el.removeEventListener("wheel", onWheel);
      el.removeEventListener("pointerdown", onDown);
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
    };
  }, [ref, gutter]);

  return width;
}
