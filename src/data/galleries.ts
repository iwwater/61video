import type { GalleryDetail, GalleryItem } from "@/types";

export function fetchGalleries(
  page: number,
  pageSize: number,
  params?: {
    tag?: string;
    sort?: string;
  }
): Promise<{ items: GalleryItem[]; total: number }> {
  const qs = new URLSearchParams({
    page: String(page),
    size: String(pageSize),
  });
  if (params?.tag) qs.set("tag", params.tag);
  if (params?.sort) qs.set("sort", params.sort);
  return apiGet<{ items: GalleryItem[]; total: number }>(
    `/api/galleries?${qs.toString()}`
  ).catch(() => ({ items: [], total: 0 }));
}

export function fetchGalleryDetail(
  id: string
): Promise<GalleryDetail | null> {
  return apiGet<GalleryDetail>(
    `/api/gallery/${encodeURIComponent(id)}`
  ).catch(() => null);
}

async function apiGet<T>(path: string): Promise<T> {
  const res = await fetch(path, { credentials: "include" });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}
