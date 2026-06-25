import { useCallback, useEffect, useRef, useState } from "react";
import { ChevronLeft, ChevronRight, Sparkles } from "lucide-react";
import { VideoCard } from "./VideoCard";
import type { VideoItem } from "@/types";

/**
 * 首页顶部"P1 精选"横滚卡片条。
 *
 * 设计目标：
 *   - 横滚 5+ 个 VideoCard（实际 mock 10 个，后续接 /api/home/promo）
 *   - 左右箭头按钮：每次滚动 ~80% 容器宽度
 *   - 移动端隐藏箭头，依赖原生 touch scroll
 *   - 复用 VideoCard，行为完全一致（hover 预览、点击进详情等）
 *
 * 性能优化：
 *   - 10 条 mock 数据从 src/data/promo.ts 拆出去，主 bundle 不再带这
 *     段静态内容。组件 mount 后通过 useEffect + setState 异步加载。
 *   - 父组件传入 videos 时直接走 props，不再触发任何异步加载。
 */
export function PromoStrip({ videos }: { videos?: VideoItem[] } = {}) {
  const fallbackItems = useAsyncPromoFallback(videos);
  const items = videos && videos.length > 0 ? videos : fallbackItems;
  const scrollerRef = useRef<HTMLDivElement | null>(null);
  const [canPrev, setCanPrev] = useState(false);
  const [canNext, setCanNext] = useState(false);

  const updateButtons = useCallback(() => {
    const el = scrollerRef.current;
    if (!el) return;
    const { scrollLeft, scrollWidth, clientWidth } = el;
    // 留 2px 缓冲避免 subpixel 抖动
    setCanPrev(scrollLeft > 2);
    setCanNext(scrollLeft + clientWidth < scrollWidth - 2);
  }, []);

  useEffect(() => {
    updateButtons();
    const el = scrollerRef.current;
    if (!el) return;
    el.addEventListener("scroll", updateButtons, { passive: true });
    window.addEventListener("resize", updateButtons);
    return () => {
      el.removeEventListener("scroll", updateButtons);
      window.removeEventListener("resize", updateButtons);
    };
  }, [updateButtons, items.length]);

  function scrollByPage(dir: 1 | -1) {
    const el = scrollerRef.current;
    if (!el) return;
    const delta = el.clientWidth * 0.8 * dir;
    el.scrollBy({ left: delta, behavior: "smooth" });
  }

  if (items.length === 0) return null;

  return (
    <section className="promo-strip" aria-label="P1 精选">
      <header className="promo-strip__head">
        <span className="promo-strip__head-icon" aria-hidden="true">
          <Sparkles size={18} />
        </span>
        <h2 className="promo-strip__head-title">P1 精选</h2>
        <span className="promo-strip__count">{items.length} 部</span>
        <div className="promo-strip__nav">
          <button
            type="button"
            className="promo-strip__nav-btn"
            onClick={() => scrollByPage(-1)}
            disabled={!canPrev}
            aria-label="向前滚动"
          >
            <ChevronLeft size={18} />
          </button>
          <button
            type="button"
            className="promo-strip__nav-btn"
            onClick={() => scrollByPage(1)}
            disabled={!canNext}
            aria-label="向后滚动"
          >
            <ChevronRight size={18} />
          </button>
        </div>
      </header>
      <div className="promo-strip__scroller" ref={scrollerRef}>
        {items.map((v, i) => (
          <div className="promo-strip__item" key={v.id}>
            <VideoCard video={v} priority={i < 4} />
          </div>
        ))}
      </div>
    </section>
  );
}

/**
 * 当父组件没有传 videos 时，组件挂载后异步加载 src/data/promo.ts 里
 * 的兜底 mock。这样首屏可以先 paint 出"加载中"骨架，mock 数据再通过
 * 单独的 chunk 进入，避免阻塞 LCP。
 */
function useAsyncPromoFallback(videos: VideoItem[] | undefined): VideoItem[] {
  const [fallback, setFallback] = useState<VideoItem[]>([]);
  useEffect(() => {
    if (videos && videos.length > 0) return;
    let cancelled = false;
    // 动态 import 让 Vite 把 mock 数据打成单独 chunk。
    void import("@/data/promo").then((mod) => {
      if (!cancelled) setFallback(mod.MOCK_PROMO_VIDEOS);
    });
    return () => {
      cancelled = true;
    };
  }, [videos]);
  return fallback;
}

export default PromoStrip;
