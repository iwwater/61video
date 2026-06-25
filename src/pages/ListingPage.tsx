import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { AppShell } from "@/components/AppShell";
import { PromoStrip } from "@/components/PromoStrip";
import { SearchPanel } from "@/components/SearchPanel";
import { TagCloud } from "@/components/TagCloud";
import { SectionHeader } from "@/components/SectionHeader";
import { SortToolbar, type ViewMode } from "@/components/SortToolbar";
import { VideoGrid } from "@/components/VideoGrid";
import { Pagination } from "@/components/Pagination";
import { fetchListing } from "@/data/videos";
import type { SortKey, VideoItem } from "@/types";

const PAGE_SIZE_DEFAULT = 24;
const PAGE_SIZE_TAG = 12;
const LISTING_STATE_PREFIX = "video-site:list-state:";

type MediaType = "all" | "video" | "audio";

type ListingState = {
  sort: SortKey;
  view: ViewMode;
  page: number;
  scrollY: number;
};

function normalizeMediaType(value: string | null): MediaType {
  if (value === "video" || value === "audio") return value;
  return "all";
}

export default function ListingPage({ forcedMediaType }: { forcedMediaType?: MediaType } = {}) {
  const [params, setParams] = useSearchParams();
  const keyword = params.get("q") ?? "";
  const tag = params.get("tag") ?? "";
  const cat = params.get("cat") ?? "";
  const urlMediaType = normalizeMediaType(params.get("type"));
  const mediaType: MediaType = forcedMediaType ?? urlMediaType;
  const listKey = useMemo(
    () => listingStateKey({ keyword, tag, cat, mediaType }),
    [keyword, tag, cat, mediaType]
  );
  const initialState = useMemo(() => readListingState(listKey), [listKey]);
  const activeListKeyRef = useRef(listKey);
  const hasLoadedListingRef = useRef(false);
  const pendingScrollYRef = useRef<number | null>(
    initialState ? initialState.scrollY : null
  );

  const [sort, setSort] = useState<SortKey>(initialState?.sort ?? "latest");
  const [view, setView] = useState<ViewMode>(initialState?.view ?? "grid");
  const [page, setPage] = useState(initialState?.page ?? 1);
  const [initialLoading, setInitialLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [items, setItems] = useState<VideoItem[]>([]);
  const [total, setTotal] = useState(0);
  const isFetching = initialLoading || refreshing;

  useEffect(() => {
    if (activeListKeyRef.current === listKey) return;
    activeListKeyRef.current = listKey;
    const saved = readListingState(listKey);
    setSort(saved?.sort ?? "latest");
    setView(saved?.view ?? "grid");
    setPage(saved?.page ?? 1);
    pendingScrollYRef.current = saved ? saved.scrollY : 0;
  }, [listKey]);

  useEffect(() => {
    document.title = keyword
      ? `搜索 "${keyword}" · 61`
      : tag
      ? `标签 ${tag} · 61`
      : cat
      ? `分类 ${cat} · 61`
      : mediaType === "audio"
      ? "音频列表 · 61"
      : "视频列表 · 61";

    let active = true;
    const isInitialLoad = !hasLoadedListingRef.current;
    if (isInitialLoad) {
      setInitialLoading(true);
    } else {
      setRefreshing(true);
    }
    fetchListing(page, tag ? PAGE_SIZE_TAG : PAGE_SIZE_DEFAULT, {
      q: keyword,
      tag,
      cat,
      mediaType,
      sort,
    }).then((r) => {
      if (!active) return;
      setItems(r.items ?? []);
      setTotal(r.total ?? 0);
      hasLoadedListingRef.current = true;
      setInitialLoading(false);
      setRefreshing(false);
    });
    return () => {
      active = false;
    };
  }, [keyword, tag, cat, mediaType, sort, page]);

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
      writeListingState(listKey, { sort, view, page, scrollY: window.scrollY });
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
  }, [listKey, sort, view, page]);

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

  const title = keyword
    ? `搜索结果：${keyword}`
    : tag
    ? `标签：${tag}`
    : cat && cat !== "all"
    ? `分类：${cat}`
    : mediaType === "audio"
    ? "音频"
    : "全部视频";

  // 用 useCallback 稳定 handleSortChange / handleViewChange / handlePageChange 的引用，
  // 这样 SortToolbar / VideoGrid / Pagination 加 memo 后才会真正生效。
  const handleSortChange = useCallback((nextSort: SortKey) => {
    pendingScrollYRef.current = 0;
    setSort(nextSort);
    setPage(1);
    window.scrollTo({ top: 0, behavior: "smooth" });
  }, []);

  const handleViewChange = useCallback((nextView: ViewMode) => {
    setView(nextView);
  }, []);

  const handlePageChange = useCallback((nextPage: number) => {
    pendingScrollYRef.current = 0;
    setPage(nextPage);
    window.scrollTo({ top: 0, behavior: "smooth" });
  }, []);

  // 抽出模块级常量，避免每次 render 都构造新字符串 + 让 VideoGrid 能更准 memo。
  const emptyText = mediaType === "audio" ? "没有找到匹配的音频" : "没有找到匹配的视频";

  function setMediaType(next: MediaType) {
    if (next === mediaType) return;
    const updated = new URLSearchParams(params);
    if (next === "all") {
      updated.delete("type");
    } else {
      updated.set("type", next);
    }
    setParams(updated, { replace: true });
  }

  return (
    <AppShell>
      <div className="container page-section">
        <PromoStrip />
        <SearchPanel />
        <TagCloud />
      </div>

      <div className="container page-section">
        <div className="list-type-tabs" role="tablist" aria-label="媒体类型">
          <button
            type="button"
            role="tab"
            aria-selected={mediaType === "all"}
            className={`list-type-tab ${mediaType === "all" ? "is-active" : ""}`}
            onClick={() => setMediaType("all")}
          >
            全部
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={mediaType === "video"}
            className={`list-type-tab ${mediaType === "video" ? "is-active" : ""}`}
            onClick={() => setMediaType("video")}
          >
            视频
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={mediaType === "audio"}
            className={`list-type-tab ${mediaType === "audio" ? "is-active" : ""}`}
            onClick={() => setMediaType("audio")}
          >
            音频
          </button>
          {mediaType === "audio" && (
            <Link to="/audio" className="list-type-tab__permalink" aria-label="音频独立页面">
              /audio
            </Link>
          )}
        </div>
        <SectionHeader title={title} extra={`共 ${total} 个`} />
        <SortToolbar
          sort={sort}
          view={view}
          onSortChange={handleSortChange}
          onViewChange={handleViewChange}
        />
        <VideoGrid
          videos={items}
          loading={initialLoading}
          compact={view === "compact"}
          skeletonCount={12}
          emptyText={emptyText}
        />
        <Pagination
          page={page}
          pageSize={tag ? PAGE_SIZE_TAG : PAGE_SIZE_DEFAULT}
          total={total}
          onChange={handlePageChange}
        />
      </div>
    </AppShell>
  );
}

function listingStateKey(filters: {
  keyword: string;
  tag: string;
  cat: string;
  mediaType: MediaType;
}): string {
  const params = new URLSearchParams();
  if (filters.keyword) params.set("q", filters.keyword);
  if (filters.tag) params.set("tag", filters.tag);
  if (filters.cat) params.set("cat", filters.cat);
  if (filters.mediaType !== "all") params.set("type", filters.mediaType);
  return `${LISTING_STATE_PREFIX}${params.toString()}`;
}

function readListingState(key: string): ListingState | null {
  try {
    const raw = window.sessionStorage.getItem(key);
    if (!raw) return null;
    const value = JSON.parse(raw) as Partial<ListingState>;
    return {
      sort: isSortKey(value.sort) ? value.sort : "latest",
      view: value.view === "compact" ? "compact" : "grid",
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

function writeListingState(key: string, state: ListingState) {
  try {
    window.sessionStorage.setItem(key, JSON.stringify(state));
  } catch {
    // Storage can be unavailable in private browsing modes.
  }
}

function isSortKey(value: unknown): value is SortKey {
  return (
    value === "latest" ||
    value === "hot" ||
    value === "recent"
  );
}
