export type AnimeParseResult = {
  title: string;
  videoUrl: string;
  videoUrls?: string[];
  thumbnail?: string;
  duration?: number;
  source: string;
  headers?: Record<string, string>;
  availableParsers?: string[];
};

export type AnimeSearchItem = {
  type: "video" | "novel" | "source" | "resource";
  id: string;
  title: string;
  subtitle?: string;
  cover?: string;
  href?: string;
  url?: string;
  source?: string;
  /** 仅 resource 类型：URL 是否为 m3u8/mp4 直链，true 可直接播放 */
  directPlay?: boolean;
  /** 仅 resource 类型：资源站 ID（用于点详情时回查） */
  siteId?: string;
  /** 仅 resource 类型：资源站侧 vod ID（用于点详情） */
  vodId?: string;
};

export type AnimeResourceDetail = {
  siteId: string;
  siteName: string;
  title: string;
  cover?: string;
  playUrl: string;
  directPlay: boolean;
};

export type AnimeSearchResult = {
  items: AnimeSearchItem[];
  total: number;
  query: string;
  localCount: number;
  remoteCount: number;
};

export type AnimeSource = {
  id: string;
  name: string;
  kind: string;
  canSearch: boolean;
  canParse: boolean;
  isIframe?: boolean;
  searchUrl?: string;
  parseUrl?: string;
  note?: string;
};

export type IframeResult = {
  url: string;
  source: string;
  name: string;
};

export async function buildIframeUrl(
  sourceId: string,
  url: string
): Promise<IframeResult> {
  const res = await fetch("/api/anime/iframe", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    credentials: "include",
    body: JSON.stringify({ sourceId, url }),
  });
  if (!res.ok) {
    const t = await res.text();
    throw new Error(`HTTP ${res.status}: ${t}`);
  }
  return res.json();
}

export async function parseAnimeUrl(url: string): Promise<AnimeParseResult> {
  const res = await fetch("/api/anime/parse", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    credentials: "include",
    body: JSON.stringify({ url }),
  });
  if (!res.ok) {
    let detail = `HTTP ${res.status}`;
    try {
      const j = await res.json();
      if (j?.error) detail = j.error;
    } catch {
      // ignore
    }
    throw new Error(detail);
  }
  return res.json();
}

export async function searchAnime(
  keyword: string,
  signal?: AbortSignal
): Promise<AnimeSearchResult> {
  const res = await fetch(
    `/api/anime/search?kw=${encodeURIComponent(keyword)}`,
    {
      credentials: "include",
      signal,
    }
  );
  if (!res.ok) {
    const detail = `HTTP ${res.status}`;
    throw new Error(detail);
  }
  return res.json();
}

export async function listAnimeSources(): Promise<AnimeSource[]> {
  const res = await fetch("/api/anime/sources", { credentials: "include" });
  if (!res.ok) return [];
  const j = await res.json();
  return j.items ?? [];
}

export async function getResourceDetail(
  siteId: string,
  vodId: string
): Promise<AnimeResourceDetail> {
  const res = await fetch(
    `/api/anime/resource/detail?site=${encodeURIComponent(
      siteId
    )}&vod=${encodeURIComponent(vodId)}`,
    { credentials: "include" }
  );
  if (!res.ok) {
    let detail = `HTTP ${res.status}`;
    try {
      const j = await res.json();
      if (j?.error) detail = j.error;
    } catch {
      // ignore
    }
    throw new Error(detail);
  }
  return res.json();
}
