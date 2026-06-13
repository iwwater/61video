import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const shortsPageSource = readFileSync(
  new URL("../src/pages/ShortsPage.tsx", import.meta.url),
  "utf8"
);
const videosDataSource = readFileSync(
  new URL("../src/data/videos.ts", import.meta.url),
  "utf8"
);

test("shorts does not keep recommendation preference from likes or watch time", () => {
  assert.doesNotMatch(shortsPageSource, /currentTime\s*>=\s*3/);
  assert.doesNotMatch(shortsPageSource, /onPreferenceReady/);
  assert.doesNotMatch(shortsPageSource, /preferredFromVideoId/);
  assert.doesNotMatch(videosDataSource, /preferredFromVideoId/);

  const match = /const handleLikeToggle[\s\S]*?const hasLiked/.exec(
    shortsPageSource
  );
  assert.ok(match, "handleLikeToggle block should be present");

  assert.doesNotMatch(match[0], /preferred/i);
  assert.match(videosDataSource, /body: JSON\.stringify\(\{ seenIds, count \}\)/);
});

test("shorts progress dragging uses immediate pointer state", () => {
  assert.match(shortsPageSource, /const scrubbingRef = useRef\(false\)/);
  assert.match(shortsPageSource, /scrubbingRef\.current = true;/);
  assert.match(shortsPageSource, /if \(!scrubbingRef\.current\) return;/);
  assert.doesNotMatch(shortsPageSource, /if \(!scrubbing\) return;/);
  assert.match(shortsPageSource, /function getSeekDuration/);
  assert.match(shortsPageSource, /onLostPointerCapture=\{handleProgressPointerEnd\}/);
});

test("shorts progress listeners rebind when deferred videos mount", () => {
  assert.match(
    shortsPageSource,
    /VIDEO_WINDOW_SIZE 会让窗口外的 slide 先以海报占位/
  );
  assert.match(shortsPageSource, /if \(!shouldMount\) \{\s*setDuration\(0\);\s*setCurrentTime\(0\);/);
  assert.match(
    shortsPageSource,
    /\}, \[shouldMount, shouldLoad, item\.id, index, isActive, muted, volume, setMuted, setVolume, onActiveReadyForPreload, onActiveNeedsPriority, onSourceCached\]\);/
  );
});

test("shorts preloads the next two original videos only after the active video has comfortable buffer", () => {
  assert.match(shortsPageSource, /const \[activeReadyForPreload, setActiveReadyForPreload\] = useState\(false\);/);
  assert.match(shortsPageSource, /const ACTIVE_PRELOAD_BUFFER_SECONDS = 12;/);
  assert.match(shortsPageSource, /const PRELOAD_AHEAD_COUNT = 2;/);
  assert.match(
    shortsPageSource,
    /const preloadOffset = index - activeIndex;[\s\S]*?preloadOffset > 0 &&[\s\S]*?preloadOffset <= PRELOAD_AHEAD_COUNT;/
  );
  assert.match(shortsPageSource, /const shouldLoad = isActiveSlide \|\| shouldPreload \|\| shouldRetainCached;/);
  assert.match(shortsPageSource, /shouldLoad=\{shouldLoad\}/);
  assert.match(shortsPageSource, /setActiveReadyForPreload\(false\);\s*setActiveIndex\(bestIndex\);/);
  assert.match(shortsPageSource, /function syncActivePreloadReadiness\(currentVideo: HTMLVideoElement\)/);
  assert.match(shortsPageSource, /if \(videoHasComfortableBuffer\(currentVideo\)\) \{\s*onActiveReadyForPreload\(index\);/);
  assert.match(shortsPageSource, /if \(isActive\) onActiveNeedsPriority\(index\);/);
  assert.match(shortsPageSource, /video\.addEventListener\("progress", handleProgress\);/);
  assert.match(shortsPageSource, /src=\{shouldLoad \? item\.videoSrc : undefined\}/);
  assert.match(shortsPageSource, /video\.removeAttribute\("src"\)/);
  assert.doesNotMatch(shortsPageSource, /src=\{shouldLoad \? item\.previewSrc/);
});

test("shorts preload grant uses high/low watermark hysteresis", () => {
  // 高水位 12s 授权、低水位 4s 收回，之间维持现状，避免阈值附近抖动
  assert.match(shortsPageSource, /const ACTIVE_PRELOAD_KEEP_SECONDS = 4;/);
  assert.match(
    shortsPageSource,
    /\} else if \(videoBufferIsCritical\(currentVideo\)\) \{[\s\S]*?onActiveNeedsPriority\(index\);/
  );
  assert.match(shortsPageSource, /function videoBufferIsCritical\(video: HTMLVideoElement\)/);
  // 已缓冲到片尾时既视为健康也不视为告急，避免临近结尾误收回授权
  assert.match(shortsPageSource, /function videoBufferedToEnd\(video: HTMLVideoElement\)/);
  assert.match(
    shortsPageSource,
    /if \(videoBufferedToEnd\(video\)\) return true;[\s\S]*?>= ACTIVE_PRELOAD_BUFFER_SECONDS;/
  );
  assert.match(
    shortsPageSource,
    /if \(videoBufferedToEnd\(video\)\) return false;[\s\S]*?< ACTIVE_PRELOAD_KEEP_SECONDS;/
  );
});

test("shorts waits for the queue end before starting a new seen round", () => {
  assert.match(
    shortsPageSource,
    /if \(roundComplete\) \{[\s\S]*?if \(remaining > 0\) return;[\s\S]*?seenIdsRef\.current = \[\];[\s\S]*?saveSeenIds\(\[\]\);/
  );
});

test("shorts keeps buffered sources inside a six video window", () => {
  assert.match(shortsPageSource, /const \[cacheableSourceIds, setCacheableSourceIds\] = useState<Set<string>>/);
  assert.match(shortsPageSource, /setCacheableSourceIds\(\(prev\) => \{/);
  assert.match(shortsPageSource, /const VIDEO_WINDOW_SIZE = 6;/);
  assert.doesNotMatch(shortsPageSource, /VIDEO_WINDOW_BACKWARD_BIAS/);
  assert.match(shortsPageSource, /const \[cacheWindowHighIndex, setCacheWindowHighIndex\] = useState\(-1\);/);
  assert.match(shortsPageSource, /setCacheWindowHighIndex\(\(prev\) => Math\.max\(prev, activeIndex\)\);/);
  assert.match(shortsPageSource, /function getVideoWindowBounds\(highestViewedIndex: number, itemCount: number\)/);
  assert.match(
    shortsPageSource,
    /const videoWindow = getVideoWindowBounds\(cacheWindowHighIndex, items\.length\);/
  );
  assert.match(
    shortsPageSource,
    /const isInCacheWindow =\s*index >= videoWindow\.start && index <= videoWindow\.end;/
  );
  assert.match(
    shortsPageSource,
    /const shouldMount = isActiveSlide \|\| isInCacheWindow \|\| shouldPreload;/
  );
  // 视频窗口内已缓冲过的视频都保留 src，来回切换均复用缓存
  assert.match(
    shortsPageSource,
    /const shouldRetainCached =\s*isInCacheWindow && !isActiveSlide && cacheableSourceIds\.has\(item\.id\);/
  );
  // 窗口内视频一旦 canplay 就标记可复用，快速划走的视频回滑也有缓存
  assert.match(
    shortsPageSource,
    /if \(shouldLoad\) onSourceCached\(item\.id\);/
  );
  // 窗口内视频只要已经产生缓冲就同样标记，授权收回时不丢弃其数据
  assert.match(
    shortsPageSource,
    /if \(shouldLoad && videoHasBufferedData\(video\)\) \{\s*onSourceCached\(item\.id\);/
  );
  const playbackBlock = /\/\/ 控制每个 video 的播放状态与音量[\s\S]*?\}, \[activeIndex, muted, volume, items\.length\]\);/.exec(shortsPageSource);
  assert.ok(playbackBlock, "parent playback effect should be present");
  assert.doesNotMatch(playbackBlock[0], /currentTime\s*=\s*0/);
  assert.match(shortsPageSource, /shouldEagerLoad=\{shouldEagerLoad\}/);
  assert.match(shortsPageSource, /preload=\{shouldLoad \? \(shouldEagerLoad \? "auto" : "metadata"\) : "none"\}/);
});

test("shorts fullscreen changes preserve the active slide", () => {
  assert.match(shortsPageSource, /const activeIndexRef = useRef\(0\)/);
  assert.match(shortsPageSource, /const ignoreIntersectionUntilRef = useRef\(0\)/);
  assert.match(
    shortsPageSource,
    /if \(Date\.now\(\) < ignoreIntersectionUntilRef\.current\) return;/
  );
  assert.match(shortsPageSource, /function scheduleFullscreenActiveRestore\(\)/);
  assert.match(shortsPageSource, /scheduleFullscreenActiveRestore\(\);\s*setIsFullscreen/);
  assert.match(
    shortsPageSource,
    /function toggleFullscreen\(\) \{\s*scheduleFullscreenActiveRestore\(\);/
  );
  assert.match(shortsPageSource, /scrollIntoView\(\{ block: "start", inline: "nearest", behavior: "auto" \}\)/);
});
