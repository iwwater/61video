import { useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { AppShell } from "@/components/AppShell";
import { SearchPanel } from "@/components/SearchPanel";
import { TagCloud } from "@/components/TagCloud";
import { SectionHeader } from "@/components/SectionHeader";
import { Pagination } from "@/components/Pagination";
import { fetchGalleries } from "@/data/galleries";
import type { GalleryItem } from "@/types";
import { Link } from "react-router-dom";
import { Image } from "lucide-react";

const PAGE_SIZE = 24;
const GALLERY_STATE_PREFIX = "video-site:gallery-state:";

type GalleryState = {
  page: number;
  scrollY: number;
};

export default function GalleriesPage() {
  const [params] = useSearchParams();
  const tag = params.get("tag") ?? "";
  const sort = params.get("sort") ?? "latest";
  const listKey = useMemo(() => galleryStateKey({ tag, sort }), [tag, sort]);
  const initialState = useMemo(() => readGalleryState(listKey), [listKey]);
  const activeListKeyRef = useRef(listKey);
  const hasLoadedRef = useRef(false);
  const pendingScrollYRef = useRef<number | null>(
    initialState ? initialState.scrollY : null
  );

  const [page, setPage] = useState(initialState?.page ?? 1);
  const [initialLoading, setInitialLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [items, setItems] = useState<GalleryItem[]>([]);
  const [total, setTotal] = useState(0);
  const isFetching = initialLoading || refreshing;

  useEffect(() => {
    if (activeListKeyRef.current === listKey) return;
    activeListKeyRef.current = listKey;
    const saved = readGalleryState(listKey);
    setPage(saved?.page ?? 1);
    pendingScrollYRef.current = saved ? saved.scrollY : 0;
  }, [listKey]);

  useEffect(() => {
    document.title = tag ? `标签 ${tag} · 图集 · 61` : "图集列表 · 61";

    let active = true;
    const isInitialLoad = !hasLoadedRef.current;
    if (isInitialLoad) {
      setInitialLoading(true);
    } else {
      setRefreshing(true);
    }
    fetchGalleries(page, PAGE_SIZE, { tag, sort }).then((r) => {
      if (!active) return;
      setItems(r.items ?? []);
      setTotal(r.total ?? 0);
      hasLoadedRef.current = true;
      setInitialLoading(false);
      setRefreshing(false);
    });
    return () => {
      active = false;
    };
  }, [tag, sort, page]);

  // Scroll restoration
  useEffect(() => {
    const previous = window.history.scrollRestoration;
    window.history.scrollRestoration = "manual";
    return () => {
      window.history.scrollRestoration = previous;
    };
  }, []);

  useEffect(() => {
    let frame = 0;
    const save = () => {
      writeGalleryState(listKey, { page, scrollY: window.scrollY });
    };
    const saveOnScroll = () => {
      if (frame) return;
      frame = window.requestAnimationFrame(() => {
        frame = 0;
        save();
      });
    };
    window.addEventListener("scroll", saveOnScroll, { passive: true });
    window.addEventListener("pagehide", save);
    save();
    return () => {
      if (frame) window.cancelAnimationFrame(frame);
      window.removeEventListener("scroll", saveOnScroll);
      window.removeEventListener("pagehide", save);
      save();
    };
  }, [listKey, page]);

  useEffect(() => {
    if (isFetching) return;
    const scrollY = pendingScrollYRef.current;
    if (scrollY === null) return;
    pendingScrollYRef.current = null;
    window.requestAnimationFrame(() => {
      window.requestAnimationFrame(() => {
        window.scrollTo({ top: scrollY, behavior: "auto" });
      });
    });
  }, [isFetching, items.length, listKey]);

  const title = tag ? `标签：${tag}` : "全部图集";

  return (
    <AppShell>
      <div className="container page-section">
        <SearchPanel />
        <TagCloud />
      </div>

      <div className="container page-section">
        <SectionHeader title={title} extra={`共 ${total} 个`} />
        <GalleryGrid items={items} loading={initialLoading} />
        <Pagination
          page={page}
          pageSize={PAGE_SIZE}
          total={total}
          onChange={(p) => {
            pendingScrollYRef.current = 0;
            setPage(p);
            window.scrollTo({ top: 0, behavior: "smooth" });
          }}
        />
      </div>
    </AppShell>
  );
}

function GalleryGrid({
  items,
  loading,
}: {
  items: GalleryItem[];
  loading: boolean;
}) {
  if (loading) {
    return (
      <div className="video-grid">
        {Array.from({ length: 12 }, (_, i) => (
          <div key={i} className="gallery-card gallery-card--skeleton">
            <div className="gallery-card__cover skeleton" />
            <div className="gallery-card__body">
              <div className="skeleton skeleton--text" style={{ width: "80%" }} />
              <div className="skeleton skeleton--text" style={{ width: "40%" }} />
            </div>
          </div>
        ))}
      </div>
    );
  }

  if (items.length === 0) {
    return (
      <div className="empty-state">
        <Image size={48} />
        <p>暂无图集</p>
      </div>
    );
  }

  return (
    <div className="video-grid">
      {items.map((item) => (
        <Link
          key={item.id}
          to={`/gallery/${encodeURIComponent(item.id)}`}
          className="gallery-card"
        >
          <div className="gallery-card__cover">
            {item.coverUrl ? (
              <img
                src={item.coverUrl}
                alt={item.title}
                loading="lazy"
                onError={(e) => {
                  (e.target as HTMLImageElement).style.display = "none";
                  (e.target as HTMLImageElement).nextElementSibling?.classList.remove("hidden");
                }}
              />
            ) : null}
            <div className={`gallery-card__cover-fallback ${item.coverUrl ? "hidden" : ""}`}>
              <Image size={48} />
            </div>
            <span className="gallery-card__count">{item.imageCount}P</span>
          </div>
          <div className="gallery-card__body">
            <h3 className="gallery-card__title">{item.title}</h3>
            <span className="gallery-card__author">{item.author}</span>
          </div>
        </Link>
      ))}
    </div>
  );
}

function galleryStateKey(filters: { tag: string; sort: string }): string {
  const params = new URLSearchParams();
  if (filters.tag) params.set("tag", filters.tag);
  if (filters.sort && filters.sort !== "latest") params.set("sort", filters.sort);
  return `${GALLERY_STATE_PREFIX}${params.toString()}`;
}

function readGalleryState(key: string): GalleryState | null {
  try {
    const raw = window.sessionStorage.getItem(key);
    if (!raw) return null;
    const value = JSON.parse(raw) as Partial<GalleryState>;
    return {
      page: typeof value.page === "number" && value.page > 0 ? value.page : 1,
      scrollY:
        typeof value.scrollY === "number" && value.scrollY > 0
          ? value.scrollY
          : 0,
    };
  } catch {
    return null;
  }
}

function writeGalleryState(key: string, state: GalleryState) {
  try {
    window.sessionStorage.setItem(key, JSON.stringify(state));
  } catch {
    // Storage can be unavailable in private browsing modes.
  }
}
