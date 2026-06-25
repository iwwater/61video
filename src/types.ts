export type VideoItem = {
  id: string;
  mediaType: "video" | "audio";
  href: string;
  title: string;
  thumbnail: string;
  previewSrc: string;
  previewDuration: number;
  previewStrategy: "teaser-file" | "sprite-frames";
  duration: string;
  /** 原始秒数（前端用于算进度条比例） */
  durationSeconds?: number;
  /** 客户端最后一次上报的 currentTime。0=未看；>=duration-30 视为看完。 */
  progressSeconds?: number;
  badges: string[];
  quality?: "SD" | "HD";
  sourceLabel?: string;
  author: string;
  views: number;
  favorites?: number;
  comments?: number;
  likes?: number;
  dislikes?: number;
  publishedAt: string;
  rating?: number;
  tags?: string[];
  category?: string;
};

export type AuthorProfile = {
  id: string;
  name: string;
  href: string;
  badges: string[];
  signupAge?: string;
  level?: number;
  points?: number;
  videoCount?: number;
  followers?: number;
  following?: number;
  isFollowing?: boolean;
};

export type CommentItem = {
  id: string;
  author: string;
  body: string;
  createdAt: string;
  likes?: number;
};

export type VideoDetail = VideoItem & {
  mediaSrc?: string;
  videoSrc: string;
  poster: string;
  description: string;
  embedUrl: string;
  points?: number;
  authorProfile: AuthorProfile;
  relatedVideos: VideoItem[];
  commentsList: CommentItem[];
};

export type PreviewState = "idle" | "intent" | "loading" | "playing" | "error";

export type SortKey = "latest" | "hot" | "recent";

export type TagItem = {
  id: string;
  label: string;
  count?: number;
};

export type CategoryItem = {
  id: string;
  label: string;
  href: string;
};

export type PromoItem = {
  id: string;
  kind: "channel" | "collection" | "event";
  label: string;
  title: string;
  meta?: string;
};

export type GalleryImageItem = {
  position: number;
  url: string;
  thumbUrl?: string;
  width?: number;
  height?: number;
  headers?: Record<string, string>;
};

export type GalleryItem = {
  id: string;
  driveId: string;
  sourceId: string;
  title: string;
  author: string;
  coverUrl: string;
  imageCount: number;
  tags: string[];
  description: string;
  hidden: boolean;
  sourceKind: string;
  publishedAt: number;
  createdAt: number;
  updatedAt: number;
};

export type GalleryDetail = GalleryItem & {
  images: GalleryImageItem[];
};

export type NovelContentType = "text" | "pdf";

export type NovelChapter = {
  id: number;
  position: number;
  title: string;
  contentType: NovelContentType;
  body?: string;
  pdfUrl?: string;
  headers?: Record<string, string>;
};

export type NovelItem = {
  id: string;
  driveId: string;
  sourceId: string;
  title: string;
  author: string;
  coverUrl: string;
  contentType: NovelContentType;
  chapterCount: number;
  tags: string[];
  description: string;
  hidden: boolean;
  sourceKind: string;
  publishedAt: number;
  createdAt: number;
  updatedAt: number;
};

export type NovelDetail = NovelItem & {
  chapters: NovelChapter[];
};
