import { http } from "./client";
import type { FileItem } from "./types";

export type StrmScrapeWriteMode = "missing_only" | "overwrite";
export type StrmScrapeItemStatus = "ok" | "miss" | "doubt";
export type StrmScrapeTVState = "ended" | "updating";
export type StrmScrapeItemListSort =
  | "title_asc"
  | "year_desc"
  | "year_asc"
  | "added_desc"
  | "added_asc";

export interface StrmScrapeItem {
  id: string;
  rel_dir: string;
  strm_name?: string;
  title: string;
  year?: number;
  media_type: string;
  status: StrmScrapeItemStatus;
  has_nfo: boolean;
  has_poster: boolean;
  has_pending?: boolean;
  manual_done?: boolean;
  tmdb_id?: string;
  poster_url?: string;
  folder_name?: string;
  file_count: number;
  ep_local?: number;
  ep_tmdb?: number;
  ep_scraped?: number;
  tv_state?: StrmScrapeTVState | string;
  added_at?: string;
}

export interface StrmScrapeProgress {
  running: boolean;
  strm_task_id: number;
  total: number;
  done: number;
  skipped: number;
  failed: number;
  message: string;
  error?: string;
  started_at?: string;
  current_item_id: string;
  item_revision: number;
  updated_item?: StrmScrapeItem;
}

export interface StrmScrapeRematchResult {
  item: StrmScrapeItem;
  started: boolean;
  progress: StrmScrapeProgress;
}

export interface StrmScrapeSettings {
  write_mode: StrmScrapeWriteMode;
  episode_info: boolean;
  fanart: boolean;
  actors: boolean;
  clearlogo: boolean;
  tmdb_api_key: string;
  tmdb_language: string;
  tmdb_api_host: string;
  tmdb_image_host: string;
  tmdb_request_interval_ms: number;
  proxy_enabled: boolean;
  proxy_url: string;
  proxy_username: string;
  proxy_password: string;
}

export interface StrmScrapeItemListQuery {
  offset?: number;
  limit?: number;
  keyword?: string;
  status?: StrmScrapeItemStatus | "";
  media_type?: "movie" | "tv" | "";
  tv_state?: StrmScrapeTVState | "";
  sort?: StrmScrapeItemListSort;
}

export interface StrmScrapeItemListStats {
  total: number;
  ok: number;
  miss: number;
  doubt: number;
}

export interface StrmScrapeItemListResult {
  items: StrmScrapeItem[];
  total: number;
  offset: number;
  limit: number;
  has_more: boolean;
  stats: StrmScrapeItemListStats;
}

export interface StrmScrapeScope {
  strm_task_id: number;
  excluded_dirs: string[];
}

export function fetchStrmScrapeScope(strmTaskId: number) {
  return http.get<StrmScrapeScope>("/admin/strm-scrape/scope", { strm_task_id: strmTaskId });
}

export function saveStrmScrapeScope(strmTaskId: number, excludedDirs: string[]) {
  return http.put<StrmScrapeScope>("/admin/strm-scrape/scope", {
    strm_task_id: strmTaskId,
    excluded_dirs: excludedDirs,
  });
}

export async function fetchStrmScrapeScopeDirectories(strmTaskId: number, parent = "") {
  const items = await http.get<Array<{ id: string; name: string; mod_time?: string }>>(
    "/admin/strm-scrape/scope/directories",
    { strm_task_id: strmTaskId, parent },
  );
  return items.map<FileItem>((item) => ({
    id: item.id,
    name: item.name,
    size: 0,
    is_dir: true,
    mod_time: item.mod_time,
  }));
}

export function fetchStrmScrapeSettings() {
  return http.get<StrmScrapeSettings>("/admin/strm-scrape/settings");
}

export function saveStrmScrapeSettings(settings: Partial<StrmScrapeSettings>) {
  return http.put<StrmScrapeSettings>("/admin/strm-scrape/settings", settings);
}

export function runStrmScrape(strmTaskId: number, writeMode?: StrmScrapeWriteMode) {
  return http.post<StrmScrapeProgress>("/admin/strm-scrape/run", {
    strm_task_id: strmTaskId,
    write_mode: writeMode,
  });
}

export function stopStrmScrape() {
  return http.post<StrmScrapeProgress>("/admin/strm-scrape/stop");
}

export function fetchStrmScrapeProgress() {
  return http.get<StrmScrapeProgress>("/admin/strm-scrape/progress");
}

export function fetchStrmScrapeItems(strmTaskId: number, query: StrmScrapeItemListQuery = {}) {
  return http.get<StrmScrapeItemListResult>("/admin/strm-scrape/items", {
    strm_task_id: strmTaskId,
    ...query,
  });
}

export function refreshStrmScrapeIndex(strmTaskId: number, query: StrmScrapeItemListQuery = {}) {
  return http.post<StrmScrapeItemListResult>("/admin/strm-scrape/refresh-index", {
    strm_task_id: strmTaskId,
    ...query,
  });
}

export function rematchStrmScrapeItem(input: {
  strm_task_id: number;
  item_id: string;
  tmdb_id: string;
  media_type: string;
  title?: string;
  year?: number;
}) {
  return http.post<StrmScrapeRematchResult>("/admin/strm-scrape/rematch", input);
}

export function markStrmScrapeNormal(input: {
  strm_task_id: number;
  item_id: string;
  media_type?: string;
  clear_match?: boolean;
}) {
  return http.post<StrmScrapeItem>("/admin/strm-scrape/mark-normal", input);
}

export function rescrapeStrmScrapeItem(input: { strm_task_id: number; item_id: string }) {
  return http.post<StrmScrapeRematchResult>("/admin/strm-scrape/rescrape", input);
}

// ===== 工作台（media-editor 精修刮削）=====

export interface StrmScrapeWorkbenchFieldSet {
  title: string;
  originaltitle: string;
  year?: number;
  plot: string;
  genre: string[];
  premiered: string;
  status: string;
  sorttitle: string;
  tmdbid: string;
  uniqueid: string;
  rating: number;
  votes: number;
  mpaa: string;
  country: string;
  tag: string[];
}

export interface StrmScrapeWorkbenchEpisodeFields {
  season: number;
  episode: number;
  title: string;
  plot: string;
  aired: string;
  thumb: string;
  runtime: number;
}

export interface StrmScrapeWorkbenchImage {
  file_path: string;
  url: string;
  url_original: string;
  width: number;
  height: number;
  language?: string;
  vote_average?: number;
}

export interface StrmScrapeWorkbenchActor {
  name: string;
  role: string;
  order: number;
  profile_path?: string;
  thumb_url?: string;
  selected: boolean;
}

export interface StrmScrapeWorkbenchLocalImage {
  exists: boolean;
  url?: string;
  rel?: string;
}

export interface StrmScrapeWorkbenchEpisode {
  season: number;
  episode: number;
  title: string;
  overview: string;
  air_date?: string;
  still_path?: string;
  still_url?: string;
  runtime?: number;
  tmdb_id?: number;
  local_nfo: boolean;
  local_thumb: boolean;
  local_thumb_url?: string;
  local_fields?: StrmScrapeWorkbenchEpisodeFields;
}

export interface StrmScrapeWorkbenchSeason {
  number: number;
  name: string;
  overview: string;
  air_date?: string;
  poster_path?: string;
  poster_url?: string;
  episode_count: number;
  local_poster: boolean;
  local_season_poster: boolean;
  posters?: StrmScrapeWorkbenchImage[];
  episodes?: StrmScrapeWorkbenchEpisode[];
}

export interface StrmScrapeWorkbenchData {
  item: StrmScrapeItem;
  media_type: string;
  tmdb_id?: string;
  local: {
    fields: StrmScrapeWorkbenchFieldSet;
    actors: StrmScrapeWorkbenchActor[];
    images: {
      poster: StrmScrapeWorkbenchLocalImage;
      fanart: StrmScrapeWorkbenchLocalImage;
      logo: StrmScrapeWorkbenchLocalImage;
      season_posters?: Record<string, StrmScrapeWorkbenchLocalImage>;
    };
  };
  tmdb: {
    fields: StrmScrapeWorkbenchFieldSet;
    actors: StrmScrapeWorkbenchActor[];
    posters: StrmScrapeWorkbenchImage[];
    backdrops: StrmScrapeWorkbenchImage[];
    logos: StrmScrapeWorkbenchImage[];
  };
  seasons?: StrmScrapeWorkbenchSeason[];
  tmdb_available: boolean;
  tmdb_error?: string;
  images_error?: string;
  credits_error?: string;
}

export interface StrmScrapeWorkbenchMediaVideo {
  codec?: string;
  profile?: string;
  width?: number;
  height?: number;
  fps?: string;
  bit_rate?: string;
  pix_fmt?: string;
  hdr?: string;
}

export interface StrmScrapeWorkbenchMediaAudio {
  codec?: string;
  channels?: number;
  channel_layout?: string;
  sample_rate?: string;
  bit_rate?: string;
  language?: string;
}

export interface StrmScrapeWorkbenchMediaSubtitle {
  codec?: string;
  language?: string;
  title?: string;
}

export interface StrmScrapeWorkbenchMediaInfo {
  filename?: string;
  source?: string;
  format?: string;
  duration?: string;
  bit_rate?: string;
  file_size?: number;
  video: StrmScrapeWorkbenchMediaVideo[];
  audio: StrmScrapeWorkbenchMediaAudio[];
  subtitles: StrmScrapeWorkbenchMediaSubtitle[];
}

export function fetchStrmScrapeWorkbench(strmTaskId: number, itemId: string, tmdbId?: string) {
  return http.get<StrmScrapeWorkbenchData>(`/admin/strm-scrape/items/${itemId}/workbench`, {
    strm_task_id: strmTaskId,
    tmdb_id: tmdbId,
  });
}

// 按模块重载工作台数据（media-editor 各模块独立「读取」同款）：
// fields = 基础信息、credits = 演职人员、images = 海报/背景/Logo、
// seasons = 全部季与分集、season = 指定一季（season 必填）、local = 本地现状（供保存后刷新）。
// 只返回该模块的数据，调用方只合併这一块，不动其他模块的未存编辑。
export type StrmScrapeWorkbenchModuleKind =
  | "fields"
  | "credits"
  | "images"
  | "seasons"
  | "season"
  | "local";

export interface StrmScrapeWorkbenchModuleData {
  fields?: StrmScrapeWorkbenchFieldSet;
  actors?: StrmScrapeWorkbenchActor[];
  posters?: StrmScrapeWorkbenchImage[];
  backdrops?: StrmScrapeWorkbenchImage[];
  logos?: StrmScrapeWorkbenchImage[];
  seasons?: StrmScrapeWorkbenchSeason[];
  season?: StrmScrapeWorkbenchSeason;
  local?: StrmScrapeWorkbenchData["local"];
  tmdb_id?: string;
}

export function fetchStrmScrapeWorkbenchModule(
  strmTaskId: number,
  itemId: string,
  module: StrmScrapeWorkbenchModuleKind,
  season?: number,
) {
  return http.get<StrmScrapeWorkbenchModuleData>(
    `/admin/strm-scrape/items/${itemId}/workbench/module`,
    { strm_task_id: strmTaskId, module, season },
  );
}

export function saveStrmScrapeWorkbenchFields(input: {
  strm_task_id: number;
  item_id: string;
  kind: "work" | "episode";
  season?: number;
  episode?: number;
  fields?: Record<string, unknown>;
  episode_fields?: StrmScrapeWorkbenchEpisodeFields;
}) {
  return http.post<StrmScrapeItem>(
    `/admin/strm-scrape/items/${input.item_id}/save-fields`,
    input,
    { strm_task_id: input.strm_task_id },
  );
}

export function setStrmScrapeWorkbenchImage(input: {
  strm_task_id: number;
  item_id: string;
  kind: "poster" | "backdrop" | "logo" | "season_poster" | "episode_thumb";
  tmdb_path?: string;
  image_url?: string;
  season?: number;
  episode?: number;
}) {
  return http.post<StrmScrapeItem>(
    `/admin/strm-scrape/items/${input.item_id}/set-image`,
    input,
    { strm_task_id: input.strm_task_id },
  );
}

export function saveStrmScrapeWorkbenchCast(input: {
  strm_task_id: number;
  item_id: string;
  actors: StrmScrapeWorkbenchActor[];
}) {
  return http.post<StrmScrapeItem>(
    `/admin/strm-scrape/items/${input.item_id}/save-cast`,
    input,
    { strm_task_id: input.strm_task_id },
  );
}

export function saveStrmScrapeWorkbenchSeries(input: {
  strm_task_id: number;
  item_id: string;
  tmdb_id?: string;
  seasons?: number[];
}) {
  return http.post<StrmScrapeItem>(
    `/admin/strm-scrape/items/${input.item_id}/save-series`,
    input,
    { strm_task_id: input.strm_task_id },
  );
}

export function matchStrmScrapeWorkbench(input: {
  strm_task_id: number;
  item_id: string;
  tmdb_id: string;
}) {
  return http.post<StrmScrapeItem>(
    `/admin/strm-scrape/items/${input.item_id}/match`,
    input,
    { strm_task_id: input.strm_task_id },
  );
}

export function uploadStrmScrapeWorkbenchImage(input: {
  strm_task_id: number;
  item_id: string;
  kind: "poster" | "backdrop" | "logo" | "season_poster" | "episode_thumb";
  data: string;
  season?: number;
  episode?: number;
}) {
  return http.post<StrmScrapeItem>(
    `/admin/strm-scrape/items/${input.item_id}/upload-image`,
    input,
    { strm_task_id: input.strm_task_id },
  );
}

export function deleteStrmScrapeWorkbenchImage(input: {
  strm_task_id: number;
  item_id: string;
  kind: "poster" | "backdrop" | "logo" | "season_poster";
  season?: number;
}) {
  return http.post<StrmScrapeItem>(
    `/admin/strm-scrape/items/${input.item_id}/delete-image`,
    input,
    { strm_task_id: input.strm_task_id },
  );
}

export function copyStrmScrapeWorkbenchImage(input: {
  strm_task_id: number;
  item_id: string;
  from: "poster" | "backdrop" | "logo" | "season_poster";
  to: "poster" | "backdrop" | "logo" | "season_poster";
  season?: number;
}) {
  return http.post<StrmScrapeItem>(
    `/admin/strm-scrape/items/${input.item_id}/copy-image`,
    input,
    { strm_task_id: input.strm_task_id },
  );
}

export function fetchStrmScrapeWorkbenchMediaInfo(strmTaskId: number, itemId: string) {
  return http.get<StrmScrapeWorkbenchMediaInfo>(
    `/admin/strm-scrape/items/${itemId}/media-info`,
    { strm_task_id: strmTaskId },
  );
}

export function saveStrmScrapeWorkbenchMediaInfo(strmTaskId: number, itemId: string) {
  return http.post<StrmScrapeWorkbenchMediaInfo>(
    `/admin/strm-scrape/items/${itemId}/media-info-save`,
    {},
    { strm_task_id: strmTaskId },
  );
}

export function fetchStrmScrapeWorkbenchEpisodeImages(
  strmTaskId: number,
  itemId: string,
  season: number,
  episode: number,
) {
  return http.get<{ stills: StrmScrapeWorkbenchImage[] }>(
    `/admin/strm-scrape/items/${itemId}/episode-images`,
    { strm_task_id: strmTaskId, season, episode },
  );
}
