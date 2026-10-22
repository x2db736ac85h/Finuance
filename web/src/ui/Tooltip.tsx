// The legacy `#vtip`: one tooltip for the whole app that follows the pointer and hides on scroll.
// Tooltips only repeat information that is visible elsewhere (PLAN §7.1).

import { useEffect, useLayoutEffect, useRef, type PointerEvent, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { create } from 'zustand';

type TipState = { content: ReactNode; x: number; y: number } | null;

const useTipStore = create<{ tip: TipState }>()(() => ({ tip: null }));

export function showTip(content: ReactNode, x: number, y: number): void {
  useTipStore.setState({ tip: { content, x, y } });
}

export function hideTip(): void {
  if (useTipStore.getState().tip) useTipStore.setState({ tip: null });
}

/** Pointer handlers that show content next to the pointer while it is over an element. */
export function useTip(content: ReactNode) {
  return {
    onPointerMove: (e: PointerEvent) => {
      showTip(content, e.clientX, e.clientY);
    },
    onPointerLeave: hideTip,
  };
}

/** Mount once. Places the tip right of and below the pointer, flipped near the window edges. */
export function TooltipLayer() {
  const tip = useTipStore((s) => s.tip);
  const ref = useRef<HTMLDivElement>(null);

  useEffect(() => {
    window.addEventListener('scroll', hideTip, { capture: true, passive: true });
    window.addEventListener('blur', hideTip);
    return () => {
      window.removeEventListener('scroll', hideTip, { capture: true });
      window.removeEventListener('blur', hideTip);
    };
  }, []);

  useLayoutEffect(() => {
    const el = ref.current;
    if (!el || !tip) return;
    const w = el.offsetWidth;
    const h = el.offsetHeight;
    let x = tip.x + 14;
    let y = tip.y + 14;
    if (x + w > window.innerWidth - 8) x = Math.max(8, tip.x - w - 14);
    if (y + h > window.innerHeight - 8) y = Math.max(8, tip.y - h - 14);
    el.style.left = `${x}px`;
    el.style.top = `${y}px`;
  }, [tip]);

  return createPortal(
    <div ref={ref} className="vtip" role="tooltip" style={tip ? { display: 'block' } : undefined}>
      {tip?.content}
    </div>,
    document.body,
  );
}
