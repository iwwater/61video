import ListingPage from "./ListingPage";

/**
 * /audio 路由入口。复用 ListingPage 逻辑，强制 mediaType=audio。
 * 视觉效果与 /list?type=audio 一致，但 URL 更短、不带查询参数。
 */
export default function AudioListingPage() {
  return <ListingPage forcedMediaType="audio" />;
}