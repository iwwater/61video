import type { VideoDetail, VideoItem } from "@/types";

// 真实后端接口调用。未配置网盘时，各接口返回空数据。
export function fetchHomeVideos(excludeIds?: string[]): Promise<VideoItem[]> {
  const qs = new URLSearchParams();
  for (const id of excludeIds ?? []) {
    if (id.trim()) qs.append("exclude", id.trim());
  }
  const suffix = qs.toString() ? `?${qs.toString()}` : "";
  return apiGet<VideoItem[]>(`/api/home${suffix}`).catch(() => []);
}

// "继续观看" rail 数据：最近 10 部看了一半的（5%~95% 进度）。
export function fetchContinueWatching(): Promise<VideoItem[]> {
  return apiGet<{ items: VideoItem[] }>(`/api/home/continue-watching`)
    .then((d) => d.items ?? [])
    .catch(() => []);
}

export type SearchItemType = "video" | "novel" | "source" | "resource";

export interface UnifiedSearchItem {
  type: SearchItemType;
  id: string;
  title: string;
  subtitle?: string;
  cover?: string;
  href?: string;
  url?: string;
  source?: string;
  directPlay?: boolean;
  siteId?: string;
  vodId?: string;
  progressSeconds?: number;
}

export interface UnifiedSearchResult {
  items: UnifiedSearchItem[];
  query: string;
  local: number;
  remote: number;
}

export function fetchUnifiedSearch(
  q: string,
  include?: "local" | "external" | "local,external"
): Promise<UnifiedSearchResult> {
  const qs = new URLSearchParams({ q });
  if (include) qs.set("include", include);
  return apiGet<UnifiedSearchResult>(`/api/search?${qs.toString()}`);
}

export function fetchListing(
  page: number,
  pageSize: number,
  params?: {
    q?: string;
    tag?: string;
    cat?: string;
    /** "video" | "audio"。空或 "all" 表示不过滤。 */
    mediaType?: string;
    sort?: string;
    includeTotal?: boolean;
  }
): Promise<{ items: VideoItem[]; total: number }> {
  const qs = new URLSearchParams({
    page: String(page),
    size: String(pageSize),
  });
  if (params?.q) qs.set("q", params.q);
  if (params?.tag) qs.set("tag", params.tag);
  if (params?.cat) qs.set("cat", params.cat);
  if (params?.mediaType && params.mediaType !== "all") {
    qs.set("media_type", params.mediaType);
  }
  if (params?.sort) qs.set("sort", params.sort);
  if (params?.includeTotal === false) qs.set("count", "false");
  return apiGet<{ items: VideoItem[]; total: number }>(
    `/api/list?${qs.toString()}`
  ).catch(() => ({ items: [], total: 0 }));
}

export function fetchVideoDetail(id: string): Promise<VideoDetail | null> {
  return apiGet<VideoDetail>(`/api/video/${encodeURIComponent(id)}`).catch(
    () => null
  );
}

export function updateVideoTags(
  id: string,
  tags: string[]
): Promise<VideoItem> {
  return apiJSON<VideoItem>(`/api/video/${encodeURIComponent(id)}/tags`, {
    method: "PUT",
    body: JSON.stringify({ tags }),
  });
}

export function hideVideo(id: string): Promise<{ ok: boolean }> {
  return apiJSON<{ ok: boolean }>(
    `/api/video/${encodeURIComponent(id)}/hide`,
    { method: "POST" }
  );
}

export function deleteVideo(
  id: string,
  options: { deleteSource?: boolean } = {}
): Promise<{ ok: boolean; deletedSource: boolean }> {
  return apiJSON<{ ok: boolean; deletedSource: boolean }>(
    `/admin/api/videos/${encodeURIComponent(id)}`,
    {
      method: "DELETE",
      body: JSON.stringify({ deleteSource: !!options.deleteSource }),
    }
  );
}

export function recordView(id: string): Promise<{ views: number }> {
  return apiJSON<{ views: number }>(
    `/api/video/${encodeURIComponent(id)}/view`,
    { method: "POST" }
  );
}

export type UploadVideoInput = {
  file: File;
  title: string;
  tags: string[];
};

export function uploadVideo(input: UploadVideoInput): Promise<VideoItem> {
  const body = new FormData();
  body.append("file", input.file);
  if (input.title.trim()) {
    body.append("title", input.title.trim());
  }
  for (const tag of input.tags) {
    body.append("tags", tag);
  }
  return apiForm<VideoItem>("/api/upload", body);
}

export type TagItem = { id: string; label: string; count?: number };

// 标签是相对静态的数据：5 分钟内重复请求一律走缓存，避免每次进首页 / 列表页都打
// /api/tags。把 TTL 从 30s 提到 5min（300_000ms）能砍掉绝大部分跨页重复请求。
const TAG_CACHE_TTL_MS = 300_000;
let cachedTags: TagItem[] | null = null;
let cachedTagsAt = 0;
let pendingTags: Promise<TagItem[]> | null = null;

export function fetchTags(): Promise<TagItem[]> {
  const now = Date.now();
  if (cachedTags && now - cachedTagsAt < TAG_CACHE_TTL_MS) {
    return Promise.resolve(cachedTags);
  }
  if (pendingTags) return pendingTags;
  pendingTags = apiGet<TagItem[]>("/api/tags")
    .then((tags) => {
      cachedTags = tags;
      cachedTagsAt = Date.now();
      return tags;
    })
    .catch(() => cachedTags ?? [])
    .finally(() => {
      pendingTags = null;
    });
  return pendingTags;
}

/**
 * 通用 30s 内存缓存。供首页 / 详情页 / "继续观看" 这类高频小查询共用：
 * 短时间内从同一组参数重复请求直接复用上次的结果，避免重复打后端。
 *
 * 用法：
 *   const fetch = withClientCache<MyResult, [MyParams]>(
 *     "my-key",
 *     30_000,
 *     (params) => apiGet<MyResult>(`/api/...${params.id}`),
 *   );
 *   await fetch({ id: "1" });
 */
const CLIENT_CACHE_TTL_MS = 30_000;

type CacheEntry<T> = {
  value: T;
  expiresAt: number;
};

const clientCache = new Map<string, CacheEntry<unknown>>();
const pendingPromises = new Map<string, Promise<unknown>>();

function hashKey(prefix: string, parts: unknown): string {
  // 简易稳定 hash：JSON.stringify 对简单对象足够。
  try {
    return `${prefix}::${JSON.stringify(parts)}`;
  } catch {
    return `${prefix}::${String(parts)}`;
  }
}

export function withClientCache<T, Args extends unknown[]>(
  prefix: string,
  ttlMs: number,
  fetcher: (...args: Args) => Promise<T>
): (...args: Args) => Promise<T> {
  return async (...args: Args): Promise<T> => {
    const key = hashKey(prefix, args);
    const now = Date.now();
    const cached = clientCache.get(key) as CacheEntry<T> | undefined;
    if (cached && cached.expiresAt > now) {
      return cached.value;
    }
    const pending = pendingPromises.get(key) as Promise<T> | undefined;
    if (pending) return pending;
    const promise = fetcher(...args)
      .then((value) => {
        clientCache.set(key, { value, expiresAt: Date.now() + ttlMs });
        return value;
      })
      .catch((err) => {
        // 失败时清除 pending，避免后续请求被错误结果污染
        pendingPromises.delete(key);
        throw err;
      })
      .finally(() => {
        pendingPromises.delete(key);
      });
    pendingPromises.set(key, promise);
    return promise;
  };
}

/**
 * 30s 缓存版本的高频数据拉取。复用上面 withClientCache。
 * - cachedHomeVideos: 首页推荐轮播（exclude 数组可能很长，但 hash 还是稳定的）
 * - cachedContinueWatching: 继续观看 rail
 * - cachedVideoDetail: 详情页基本信息
 */
export const cachedHomeVideos = withClientCache<VideoItem[], [string[] | undefined]>(
  "home",
  CLIENT_CACHE_TTL_MS,
  (exclude) => fetchHomeVideos(exclude)
);

export const cachedContinueWatching = withClientCache<VideoItem[], []>(
  "continue-watching",
  CLIENT_CACHE_TTL_MS,
  () => fetchContinueWatching()
);

export const cachedVideoDetail = withClientCache<VideoDetail | null, [string]>(
  "video-detail",
  CLIENT_CACHE_TTL_MS,
  (id) => fetchVideoDetail(id)
);

/** 短视频模式单条记录。比 VideoItem 多 videoSrc / poster。 */
export type ShortsItem = VideoItem & {
  videoSrc: string;
  poster: string;
};

/** 短视频"取下一批"接口的响应。 */
export type ShortsNextResponse = {
  items: ShortsItem[];
  total: number;
  /** true 表示这批返回少于 count，前端播放完毕后应清空 seenIds 开新一轮 */
  roundComplete: boolean;
};

/**
 * 拉取短视频流的下一批候选。把当前轮已看过的 video id 列表传给后端，
 * 服务器从未在列表中的视频里随机抽 count 条返回。
 *
 * 失败时返回空批 + roundComplete=false，由调用方决定是否重试。
 */
export function fetchShortsNext(
  seenIds: string[],
  count: number
): Promise<ShortsNextResponse> {
  return apiJSON<ShortsNextResponse>("/api/shorts/next", {
    method: "POST",
    body: JSON.stringify({ seenIds, count }),
  }).catch(() => ({ items: [], total: 0, roundComplete: false }));
}

async function apiGet<T>(path: string): Promise<T> {
  const res = await fetch(path, { credentials: "include" });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

async function apiJSON<T>(path: string, init: RequestInit): Promise<T> {
  const res = await fetch(path, {
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    ...init,
  });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

async function apiForm<T>(path: string, body: FormData): Promise<T> {
  const res = await fetch(path, {
    method: "POST",
    credentials: "include",
    body,
  });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}
