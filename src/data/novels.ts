import type { NovelContentType, NovelDetail, NovelItem, NovelChapter } from "@/types";

export function fetchNovels(
  page: number,
  pageSize: number,
  options: { tag?: string; sort?: string; contentType?: NovelContentType } = {}
): Promise<{ items: NovelItem[]; total: number }> {
  const params = new URLSearchParams();
  params.set("page", String(page));
  params.set("size", String(pageSize));
  if (options.tag) params.set("tag", options.tag);
  if (options.sort) params.set("sort", options.sort);
  if (options.contentType) params.set("contentType", options.contentType);
  return apiGet<{ items: NovelItem[]; total: number }>(
    `/api/novels?${params.toString()}`
  ).catch(() => ({ items: [], total: 0 }));
}

export function fetchNovelDetail(id: string): Promise<NovelDetail | null> {
  return apiGet<NovelDetail>(`/api/novel/${encodeURIComponent(id)}`).catch(
    () => null
  );
}

export function fetchNovelChapter(
  id: string,
  position: number
): Promise<NovelChapter | null> {
  return apiGet<NovelChapter>(
    `/api/novel/${encodeURIComponent(id)}/chapter/${position}`
  ).catch(() => null);
}

export type CreateNovelChapterInput = {
  position?: number;
  title: string;
  contentType?: "text" | "pdf";
  body?: string;
  pdfUrl?: string;
  headers?: Record<string, string>;
};

export type CreateNovelInput = {
  id: string;
  title: string;
  author?: string;
  coverUrl?: string;
  contentType: "text" | "pdf";
  tags?: string[];
  description?: string;
  chapters: CreateNovelChapterInput[];
};

export async function createNovel(
  input: CreateNovelInput
): Promise<NovelDetail> {
  const res = await fetch("/api/novels", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    credentials: "include",
    body: JSON.stringify({
      ...input,
      tags: input.tags ?? [],
    }),
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

export async function deleteNovel(id: string): Promise<void> {
  await fetch(`/api/novel/${encodeURIComponent(id)}`, {
    method: "DELETE",
    credentials: "include",
  });
}

async function apiGet<T>(path: string): Promise<T> {
  const res = await fetch(path, { credentials: "include" });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}
