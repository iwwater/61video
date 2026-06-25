import { Suspense, lazy } from "react";
import { Navigate, Route, Routes } from "react-router-dom";
import { SkyStarfield } from "@/components/SkyStarfield";
import { RequireAuth } from "@/admin/RequireAuth";

const HomePage = lazy(() => import("@/pages/HomePage"));
const ListingPage = lazy(() => import("@/pages/ListingPage"));
const AudioListingPage = lazy(() => import("@/pages/AudioListingPage"));
const ShortsPage = lazy(() => import("@/pages/ShortsPage"));
const UploadPage = lazy(() => import("@/pages/UploadPage"));
const VideoDetailPage = lazy(() => import("@/pages/VideoDetailPage"));
const GalleriesPage = lazy(() => import("@/pages/GalleriesPage"));
const GalleryDetailPage = lazy(() => import("@/pages/GalleryDetailPage"));
const NovelsPage = lazy(() => import("@/pages/NovelsPage"));
const NovelDetailPage = lazy(() => import("@/pages/NovelDetailPage"));
const AnimeParsePage = lazy(() => import("@/pages/AnimeParsePage"));
const SearchPage = lazy(() => import("@/pages/SearchPage"));

const LoginPage = lazy(() =>
  import("@/admin/LoginPage").then((module) => ({ default: module.LoginPage }))
);
const AdminLayout = lazy(() =>
  import("@/admin/AdminLayout").then((module) => ({
    default: module.AdminLayout,
  }))
);
const DrivesPage = lazy(() =>
  import("@/admin/DrivesPage").then((module) => ({ default: module.DrivesPage }))
);
const CrawlersPage = lazy(() =>
  import("@/admin/CrawlersPage").then((module) => ({
    default: module.CrawlersPage,
  }))
);
const VideosPage = lazy(() =>
  import("@/admin/VideosPage").then((module) => ({ default: module.VideosPage }))
);
const TagsPage = lazy(() =>
  import("@/admin/TagsPage").then((module) => ({ default: module.TagsPage }))
);
const ThemePage = lazy(() =>
  import("@/admin/ThemePage").then((module) => ({ default: module.ThemePage }))
);
const ParseSourcesPage = lazy(() =>
  import("@/admin/ParseSourcesPage").then((module) => ({
    default: module.ParseSourcesPage,
  }))
);
const ResourceSitesPage = lazy(() =>
  import("@/admin/ResourceSitesPage").then((module) => ({
    default: module.ResourceSitesPage,
  }))
);
const ImageSetsPage = lazy(() =>
  import("@/admin/ImageSetsPage").then((module) => ({
    default: module.ImageSetsPage,
  }))
);
const NovelsAdminPage = lazy(() =>
  import("@/admin/NovelsAdminPage").then((module) => ({
    default: module.NovelsAdminPage,
  }))
);

export default function App() {
  return (
    <>
      {/* 星空蓝主题的固定位置星星层，仅在 data-theme="sky" 下可见 */}
      <SkyStarfield />
      <Suspense fallback={null}>
        <Routes>
          <Route path="/login" element={<LoginPage />} />

          {/* 主站需要登录 */}
          <Route
            path="/"
            element={
              <RequireAuth>
                <HomePage />
              </RequireAuth>
            }
          />
          <Route
            path="/search"
            element={
              <RequireAuth>
                <SearchPage />
              </RequireAuth>
            }
          />
          <Route
            path="/list"
            element={
              <RequireAuth>
                <ListingPage />
              </RequireAuth>
            }
          />
          <Route
            path="/audio"
            element={
              <RequireAuth>
                <AudioListingPage />
              </RequireAuth>
            }
          />
          <Route
            path="/shorts"
            element={
              <RequireAuth>
                <ShortsPage />
              </RequireAuth>
            }
          />
          <Route
            path="/upload"
            element={
              <RequireAuth>
                <UploadPage />
              </RequireAuth>
            }
          />
          <Route
            path="/video/:id"
            element={
              <RequireAuth>
                <VideoDetailPage />
              </RequireAuth>
            }
          />
          <Route
            path="/galleries"
            element={
              <RequireAuth>
                <GalleriesPage />
              </RequireAuth>
            }
          />
          <Route
            path="/gallery/:id"
            element={
              <RequireAuth>
                <GalleryDetailPage />
              </RequireAuth>
            }
          />
          <Route
            path="/novels"
            element={
              <RequireAuth>
                <NovelsPage />
              </RequireAuth>
            }
          />
          <Route
            path="/novel/:id"
            element={
              <RequireAuth>
                <NovelDetailPage />
              </RequireAuth>
            }
          />
          <Route
            path="/anime"
            element={
              <RequireAuth>
                <AnimeParsePage />
              </RequireAuth>
            }
          />

          {/* 管理后台也需要登录 */}
          <Route
            path="/admin"
            element={
              <RequireAuth>
                <AdminLayout />
              </RequireAuth>
            }
          >
            <Route index element={<Navigate to="/admin/drives" replace />} />
            <Route path="drives" element={<DrivesPage />} />
            <Route path="crawlers" element={<CrawlersPage />} />
            <Route path="videos" element={<VideosPage />} />
            <Route path="tags" element={<TagsPage />} />
            <Route path="theme" element={<ThemePage />} />
            <Route path="parse-sources" element={<ParseSourcesPage />} />
            <Route path="resource-sites" element={<ResourceSitesPage />} />
            <Route path="image-sets" element={<ImageSetsPage />} />
            <Route path="novels" element={<NovelsAdminPage />} />
          </Route>

          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </Suspense>
    </>
  );
}
