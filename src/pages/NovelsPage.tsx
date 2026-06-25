import { useEffect, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { AppShell } from "@/components/AppShell";
import { SearchPanel } from "@/components/SearchPanel";
import { TagCloud } from "@/components/TagCloud";
import { SectionHeader } from "@/components/SectionHeader";
import { Pagination } from "@/components/Pagination";
import { fetchNovels } from "@/data/novels";
import type { NovelContentType, NovelItem } from "@/types";
import { BookOpen, FileText } from "lucide-react";

const PAGE_SIZE = 24;
const NOVEL_STATE_PREFIX = "video-site:novel-state:";

type NovelState = { page: number; scrollY: number };

type Tab = "all" | "text" | "pdf";

export default function NovelsPage() {
  const [params] = useSearchParams();
  const tag = params.get("tag") ?? "";
  const sort = params.get("sort") ?? "latest";
  const initialTab = (params.get("tab") as Tab) ?? "all";
  const [tab, setTab] = useState<Tab>(initialTab);

  const listKey = useMemo(
    () => novelStateKey({ tag, sort, tab }),
    [tag, sort, tab]
  );
  const initialState = useMemo(() => readNovelState(listKey), [listKey]);

  const [page, setPage] = useState(initialState?.page ?? 1);
  const [items, setItems] = useState<NovelItem[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    document.title = tag ? `标签 ${tag} · 小说 · 61` : "小说列表 · 61";
  }, [tag]);

  useEffect(() => {
    let active = true;
    setLoading(true);
    const ct: NovelContentType | undefined =
      tab === "text" ? "text" : tab === "pdf" ? "pdf" : undefined;
    fetchNovels(page, PAGE_SIZE, { tag, sort, contentType: ct }).then((r) => {
      if (!active) return;
      setItems(r.items ?? []);
      setTotal(r.total ?? 0);
      setLoading(false);
    });
    return () => {
      active = false;
    };
  }, [tag, sort, page, tab]);

  useEffect(() => {
    const previous = window.history.scrollRestoration;
    window.history.scrollRestoration = "manual";
    return () => {
      window.history.scrollRestoration = previous;
    };
  }, []);

  useEffect(() => {
    const save = () => {
      writeNovelState(listKey, { page, scrollY: window.scrollY });
    };
    const onScroll = () => {
      window.requestAnimationFrame(save);
    };
    window.addEventListener("scroll", onScroll, { passive: true });
    window.addEventListener("pagehide", save);
    save();
    return () => {
      window.removeEventListener("scroll", onScroll);
      window.removeEventListener("pagehide", save);
      save();
    };
  }, [listKey, page]);

  const title = tag ? `标签：${tag}` : "全部小说";

  return (
    <AppShell>
      <div className="container page-section">
        <SearchPanel />
        <TagCloud />
      </div>

      <div className="container page-section">
        <div className="section-header section-header--with-tabs">
          <SectionHeader title={title} extra={`共 ${total} 本`} />
          <div className="novel-tabs" role="tablist">
            {(["all", "text", "pdf"] as Tab[]).map((t) => (
              <button
                key={t}
                role="tab"
                aria-selected={tab === t}
                className={`novel-tab ${tab === t ? "is-active" : ""}`}
                onClick={() => {
                  setTab(t);
                  setPage(1);
                }}
              >
                {t === "all" ? "全部" : t === "text" ? "文本" : "PDF"}
              </button>
            ))}
          </div>
        </div>
        <NovelGrid items={items} loading={loading} />
        <Pagination
          page={page}
          pageSize={PAGE_SIZE}
          total={total}
          onChange={(p) => {
            setPage(p);
            window.scrollTo({ top: 0, behavior: "smooth" });
          }}
        />
      </div>
    </AppShell>
  );
}

function NovelGrid({
  items,
  loading,
}: {
  items: NovelItem[];
  loading: boolean;
}) {
  if (loading) {
    return (
      <div className="video-grid">
        {Array.from({ length: 8 }, (_, i) => (
          <div key={i} className="novel-card novel-card--skeleton">
            <div className="novel-card__cover skeleton" />
            <div className="novel-card__body">
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
        <BookOpen size={48} />
        <p>暂无小说</p>
      </div>
    );
  }
  return (
    <div className="video-grid">
      {items.map((item) => (
        <Link
          key={item.id}
          to={`/novel/${encodeURIComponent(item.id)}`}
          className="novel-card"
        >
          <div className="novel-card__cover">
            {item.coverUrl ? (
              <img
                src={item.coverUrl}
                alt={item.title}
                loading="lazy"
                onError={(e) => {
                  (e.target as HTMLImageElement).style.display = "none";
                  (e.target as HTMLImageElement).nextElementSibling?.classList.remove(
                    "hidden"
                  );
                }}
              />
            ) : null}
            <div
              className={`novel-card__cover-fallback ${
                item.coverUrl ? "hidden" : ""
              }`}
            >
              {item.contentType === "pdf" ? (
                <FileText size={48} />
              ) : (
                <BookOpen size={48} />
              )}
            </div>
            <span className="novel-card__type">
              {item.contentType === "pdf" ? "PDF" : "TXT"}
            </span>
          </div>
          <div className="novel-card__body">
            <h3 className="novel-card__title">{item.title}</h3>
            <span className="novel-card__author">{item.author}</span>
            <span className="novel-card__chapters">{item.chapterCount} 章</span>
          </div>
        </Link>
      ))}
    </div>
  );
}

function novelStateKey(filters: {
  tag: string;
  sort: string;
  tab: Tab;
}): string {
  const params = new URLSearchParams();
  if (filters.tag) params.set("tag", filters.tag);
  if (filters.sort && filters.sort !== "latest") params.set("sort", filters.sort);
  if (filters.tab && filters.tab !== "all") params.set("tab", filters.tab);
  return `${NOVEL_STATE_PREFIX}${params.toString()}`;
}

function readNovelState(key: string): NovelState | null {
  try {
    const raw = window.sessionStorage.getItem(key);
    if (!raw) return null;
    const value = JSON.parse(raw) as Partial<NovelState>;
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

function writeNovelState(key: string, state: NovelState) {
  try {
    window.sessionStorage.setItem(key, JSON.stringify(state));
  } catch {
    // ignore
  }
}
