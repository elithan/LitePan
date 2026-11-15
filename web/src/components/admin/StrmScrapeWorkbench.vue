<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { getApiErrorMessage } from "@/api/client";
import { searchMediaOrganizeTmdb, type MediaOrganizeTmdbSearchHit } from "@/api/mediaOrganize";
import {
  copyStrmScrapeWorkbenchImage,
  deleteStrmScrapeWorkbenchImage,
  fetchStrmScrapeWorkbench,
  fetchStrmScrapeWorkbenchEpisodeImages,
  fetchStrmScrapeWorkbenchMediaInfo,
  matchStrmScrapeWorkbench,
  saveStrmScrapeWorkbenchCast,
  saveStrmScrapeWorkbenchFields,
  saveStrmScrapeWorkbenchMediaInfo,
  saveStrmScrapeWorkbenchSeries,
  setStrmScrapeWorkbenchImage,
  uploadStrmScrapeWorkbenchImage,
  type StrmScrapeItem,
  type StrmScrapeWorkbenchActor,
  type StrmScrapeWorkbenchData,
  type StrmScrapeWorkbenchEpisode,
  type StrmScrapeWorkbenchEpisodeFields,
  type StrmScrapeWorkbenchFieldSet,
  type StrmScrapeWorkbenchImage,
  type StrmScrapeWorkbenchLocalImage,
  type StrmScrapeWorkbenchMediaInfo,
} from "@/api/strmScrape";
import AppButton from "@/components/base/AppButton.vue";
import AppModal from "@/components/base/AppModal.vue";
import BusySpinner from "@/components/base/BusySpinner.vue";
import { confirm } from "@/composables/useConfirm";
import { toast } from "@/composables/useToast";
import { hitId, hitKey, hitPosterURL, hitTitle, hitYear } from "@/utils/tmdbHit";

const props = defineProps<{
  open: boolean;
  taskId: number | null;
  item: StrmScrapeItem | null;
}>();
const emit = defineEmits<{ close: []; saved: [] }>();

type FieldKind = "text" | "number" | "textarea" | "list";
interface FieldRowDef {
  key: string;
  label: string;
  kind: FieldKind;
}

// media-editor 的 8 个对照字段（baseFields）：只有这些进对照表与保存。
const FIELD_ROWS: FieldRowDef[] = [
  { key: "title", label: "标题", kind: "text" },
  { key: "originaltitle", label: "原标题", kind: "text" },
  { key: "plot", label: "简介", kind: "textarea" },
  { key: "year", label: "年份", kind: "number" },
  { key: "premiered", label: "首播 / 上映", kind: "text" },
  { key: "status", label: "状态", kind: "text" },
  { key: "genre", label: "类型", kind: "list" },
  { key: "rating", label: "评分", kind: "number" },
];

type ImageKind = "poster" | "backdrop" | "logo" | "season_poster" | "episode_thumb";
const IMAGE_PICKERS: Array<{ kind: "poster" | "backdrop" | "logo"; label: string }> = [
  { kind: "poster", label: "海报" },
  { kind: "backdrop", label: "背景图" },
  { kind: "logo", label: "透明 Logo" },
];

function emptyFields(): StrmScrapeWorkbenchFieldSet {
  return {
    title: "",
    originaltitle: "",
    year: undefined,
    plot: "",
    genre: [],
    premiered: "",
    status: "",
    sorttitle: "",
    tmdbid: "",
    uniqueid: "",
    rating: 0,
    votes: 0,
    mpaa: "",
    country: "",
    tag: [],
  };
}

function cloneFields(f: StrmScrapeWorkbenchFieldSet): StrmScrapeWorkbenchFieldSet {
  return { ...f, genre: [...(f.genre || [])], tag: [...(f.tag || [])] };
}

const data = ref<StrmScrapeWorkbenchData | null>(null);
const loading = ref(false);
const loadError = ref("");
const savingFields = ref(false);
const savingCast = ref(false);
const savingSeries = ref(false);
const settingImageKey = ref("");
const saveFields = ref<StrmScrapeWorkbenchFieldSet>(emptyFields());
const castSelection = ref<StrmScrapeWorkbenchActor[]>([]);
const newActorName = ref("");
const openEpisodeKey = ref("");
const episodeEdits = ref<Record<string, StrmScrapeWorkbenchEpisodeFields>>({});
const episodeStills = ref<Record<string, StrmScrapeWorkbenchImage[]>>({});
const episodeStillsLoading = ref<Record<string, boolean>>({});
const selectedSeasons = ref<Set<number>>(new Set());
const mediaInfo = ref<StrmScrapeWorkbenchMediaInfo | null>(null);
const mediaInfoLoading = ref(false);
const mediaInfoError = ref("");
const savingMediaInfo = ref(false);
const rematchOpen = ref(false);
const rematchQuery = ref("");
const rematchCandidates = ref<MediaOrganizeTmdbSearchHit[]>([]);
const rematchSearching = ref(false);
const rematchApplying = ref(false);
const selectedCandidateKey = ref("");
const imageTab = ref<Record<string, number>>({});
const imageExpanded = ref<Record<string, boolean>>({});
const uploadingImageKey = ref("");
const deletingImageKey = ref("");
const copyingSeason = ref(-1);
const uploadInput = ref<HTMLInputElement | null>(null);
const uploadContext = ref<{ kind: ImageKind; season?: number; episode?: number } | null>(null);

function fieldsRecord(f: StrmScrapeWorkbenchFieldSet | undefined): Record<string, unknown> {
  return (f || emptyFields()) as unknown as Record<string, unknown>;
}

function fieldText(key: string, source: "local" | "tmdb" | "save"): string {
  const rec =
    source === "local"
      ? fieldsRecord(data.value?.local.fields)
      : source === "tmdb"
        ? fieldsRecord(data.value?.tmdb.fields)
        : fieldsRecord(saveFields.value);
  const v = rec[key];
  if (Array.isArray(v)) return v.join("，");
  if (v === undefined || v === null) return "";
  return String(v);
}

// TMDB 值是否为空（空值回退本地，不采用）：文本为空、列表为空、年份未填、评分/票数为 0。
function tmdbFieldEmpty(key: string): boolean {
  if (!data.value) return true;
  const v = fieldsRecord(data.value.tmdb.fields)[key];
  if (Array.isArray(v)) return v.length === 0;
  if (v === undefined || v === null) return true;
  if (typeof v === "number") return v <= 0;
  return String(v).trim() === "";
}

function setFieldValue(key: string, raw: string) {
  const row = FIELD_ROWS.find((r) => r.key === key);
  if (!row) return;
  const target = saveFields.value as unknown as Record<string, unknown>;
  if (row.kind === "list") {
    target[key] = raw
      .split(/[,，、]/)
      .map((s) => s.trim())
      .filter(Boolean);
    return;
  }
  if (row.kind === "number") {
    if (raw.trim() === "") {
      target[key] = key === "year" ? undefined : 0;
      return;
    }
    const n = Number(raw);
    target[key] = Number.isNaN(n) ? (key === "year" ? undefined : 0) : n;
    return;
  }
  target[key] = raw;
}

function applyTmdbField(key: string) {
  const row = FIELD_ROWS.find((r) => r.key === key);
  if (!row || !data.value || tmdbFieldEmpty(key)) return;
  const src = fieldsRecord(data.value.tmdb.fields)[key];
  const target = saveFields.value as unknown as Record<string, unknown>;
  if (row.kind === "list") {
    target[key] = Array.isArray(src) ? [...src] : [];
    return;
  }
  target[key] = src === undefined || src === null ? (key === "year" ? undefined : 0) : src;
}

function applyAllTmdb() {
  for (const row of FIELD_ROWS) applyTmdbField(row.key);
}

// 只有与本地不同的字段才提交（media-editor selectedFields 同款）；
// 年份被清空时用 null 表示“写空”，避免 JSON 丢掉 undefined。
function changedFields(): Record<string, unknown> {
  if (!data.value) return {};
  const out: Record<string, unknown> = {};
  const local = fieldsRecord(data.value.local.fields);
  const save = fieldsRecord(saveFields.value);
  for (const row of FIELD_ROWS) {
    const key = row.key;
    const sv = save[key];
    const lv = local[key];
    if (row.kind === "list") {
      const s = Array.isArray(sv) ? sv : [];
      const l = Array.isArray(lv) ? lv : [];
      if (JSON.stringify(s) !== JSON.stringify(l)) out[key] = s;
      continue;
    }
    if (key === "year") {
      const sn = sv === undefined || sv === null ? undefined : Number(sv);
      const ln = lv === undefined || lv === null ? undefined : Number(lv);
      if (sn !== ln) out[key] = sn === undefined ? null : sn;
      continue;
    }
    if (row.kind === "number") {
      const sn = sv === undefined || sv === null ? 0 : Number(sv);
      const ln = lv === undefined || lv === null ? 0 : Number(lv);
      if (sn !== ln) out[key] = sn;
      continue;
    }
    const ss = sv === undefined || sv === null ? "" : String(sv);
    const ls = lv === undefined || lv === null ? "" : String(lv);
    if (ss !== ls) out[key] = ss;
  }
  return out;
}

function escapeXml(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

// NFO 预览只展示改动的字段 + 已选演员（姓名 + 角色），与 media-editor 一致。
const nfoPreview = computed(() => {
  if (!data.value) return "";
  const changed = changedFields();
  const root = data.value.media_type === "tv" ? "tvshow" : "movie";
  const lines: string[] = ['<?xml version="1.0" encoding="UTF-8"?>', `<${root}>`];
  for (const row of FIELD_ROWS) {
    if (!(row.key in changed)) continue;
    const v = changed[row.key];
    const text = Array.isArray(v) ? v.join(", ") : String(v ?? "");
    lines.push(`  <${row.key}>${escapeXml(text)}</${row.key}>`);
  }
  for (const a of castSelection.value.filter((x) => x.selected)) {
    lines.push(
      `  <actor><name>${escapeXml(a.name)}</name><role>${escapeXml(a.role || "")}</role></actor>`,
    );
  }
  lines.push(`</${root}>`);
  return lines.join("\n");
});

function localImage(kind: "poster" | "backdrop" | "logo"): StrmScrapeWorkbenchLocalImage | undefined {
  if (!data.value) return undefined;
  if (kind === "poster") return data.value.local.images.poster;
  if (kind === "backdrop") return data.value.local.images.fanart;
  return data.value.local.images.logo;
}

function tmdbImages(kind: "poster" | "backdrop" | "logo"): StrmScrapeWorkbenchImage[] {
  if (!data.value) return [];
  if (kind === "poster") return data.value.tmdb.posters || [];
  if (kind === "backdrop") return data.value.tmdb.backdrops || [];
  return data.value.tmdb.logos || [];
}

// ---- 图片按语言分组（media-editor imgBlock 同款）：中文、英语、无语言、其他；每组 10 张 + 展开 ----
interface ImageGroup {
  key: string;
  label: string;
  items: StrmScrapeWorkbenchImage[];
}

function languageGroupKey(code?: string): string {
  const c = (code || "").toLowerCase();
  if (["zh", "cn", "yue"].includes(c)) return "zh";
  return c;
}

function languageLabel(code: string): string {
  if (code === "zh") return "汉语";
  if (code === "") return "无语言";
  try {
    const names = new Intl.DisplayNames(["zh-CN"], { type: "language" });
    return names.of(code) || code.toUpperCase();
  } catch {
    return code.toUpperCase();
  }
}

function groupImages(images: StrmScrapeWorkbenchImage[]): ImageGroup[] {
  const groups = new Map<string, StrmScrapeWorkbenchImage[]>();
  for (const img of images) {
    const key = languageGroupKey(img.language);
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key)!.push(img);
  }
  const rank = (k: string) => (k === "zh" ? 0 : k === "en" ? 1 : k === "" ? 2 : 3);
  return [...groups.entries()]
    .sort(([a], [b]) => rank(a) - rank(b) || languageLabel(a).localeCompare(languageLabel(b), "zh-CN"))
    .map(([key, items]) => ({ key, label: languageLabel(key), items }));
}

function groupTabKey(pickerKey: string): string {
  return pickerKey;
}

function visibleGroupImages(pickerKey: string, groups: ImageGroup[]): StrmScrapeWorkbenchImage[] {
  const idx = imageTab.value[groupTabKey(pickerKey)] || 0;
  const group = groups[idx] || groups[0];
  if (!group) return [];
  if (imageExpanded.value[`${pickerKey}:${group.key}`]) return group.items;
  return group.items.slice(0, 10);
}

function groupRestCount(pickerKey: string, groups: ImageGroup[]): number {
  const idx = imageTab.value[groupTabKey(pickerKey)] || 0;
  const group = groups[idx] || groups[0];
  if (!group) return 0;
  return Math.max(0, group.items.length - 10);
}

function groupExpanded(pickerKey: string, groups: ImageGroup[]): boolean {
  const idx = imageTab.value[groupTabKey(pickerKey)] || 0;
  const group = groups[idx] || groups[0];
  if (!group) return false;
  return Boolean(imageExpanded.value[`${pickerKey}:${group.key}`]);
}

function toggleGroupExpand(pickerKey: string, groups: ImageGroup[]) {
  const idx = imageTab.value[groupTabKey(pickerKey)] || 0;
  const group = groups[idx] || groups[0];
  if (!group) return;
  const key = `${pickerKey}:${group.key}`;
  imageExpanded.value[key] = !imageExpanded.value[key];
}

// ---- 精修顶部 hero：目前已存的本地图片（与图片选择器同一来源）----
// 设图 / 上传 / 删除后都会 reloadAfterSave → loadWorkbench 重拉 data，
// hero 跟着即时更新，不用额外维护状态。
const heroPosterUrl = computed(() => {
  const img = localImage("poster");
  return img?.exists && img.url ? img.url : "";
});
const heroBackdropUrl = computed(() => {
  const img = localImage("backdrop");
  return img?.exists && img.url ? img.url : "";
});
const heroLogoUrl = computed(() => {
  const img = localImage("logo");
  return img?.exists && img.url ? img.url : "";
});
const heroYear = computed(
  () => data.value?.item.year || data.value?.local.fields.year || undefined,
);
const heroBgStyle = computed(() =>
  heroBackdropUrl.value ? { backgroundImage: `url("${heroBackdropUrl.value}")` } : undefined,
);
const heroMeta = computed(() => {
  if (!data.value) return "";
  const parts: string[] = [];
  if (heroYear.value) parts.push(String(heroYear.value));
  parts.push(data.value.media_type === "tv" ? "剧集" : "电影");
  parts.push(`TMDB ${data.value.tmdb_id || "-"}`);
  parts.push(data.value.item.rel_dir);
  return parts.join(" · ");
});

// Logo 预览用 w300，其余 w500（media-editor imgBlock 同款）。
function previewUrl(img: StrmScrapeWorkbenchImage, kind: "poster" | "backdrop" | "logo"): string {
  if (kind === "logo") return img.url.replace("/w500/", "/w300/");
  return img.url;
}

function episodeKey(season: number, episode: number): string {
  return `${season}-${episode}`;
}

function episodeEdit(ep: StrmScrapeWorkbenchEpisode): StrmScrapeWorkbenchEpisodeFields {
  const key = episodeKey(ep.season, ep.episode);
  if (!episodeEdits.value[key]) {
    const local = ep.local_fields;
    episodeEdits.value[key] = {
      season: ep.season,
      episode: ep.episode,
      title: local?.title || ep.title,
      plot: local?.plot || ep.overview,
      aired: local?.aired || ep.air_date || "",
      thumb: local?.thumb || "",
      runtime: local?.runtime || ep.runtime || 0,
    };
  }
  return episodeEdits.value[key];
}

function toggleEpisode(season: number, episode: number) {
  const key = episodeKey(season, episode);
  openEpisodeKey.value = openEpisodeKey.value === key ? "" : key;
}

async function loadWorkbench(tmdbId?: string) {
  if (!props.open || !props.item || !props.taskId) return;
  loading.value = true;
  loadError.value = "";
  try {
    const result = await fetchStrmScrapeWorkbench(props.taskId, props.item.id, tmdbId);
    data.value = result;
    // 保存值初始化：TMDB 有值用 TMDB，否则回退本地（media-editor saveValue 同款）。
    const next = cloneFields(result.local.fields);
    const tmdbRec = fieldsRecord(result.tmdb.fields);
    for (const row of FIELD_ROWS) {
      const tv = tmdbRec[row.key];
      if (row.kind === "list") {
        if (Array.isArray(tv) && tv.length) (next as unknown as Record<string, unknown>)[row.key] = [...tv];
      } else if (row.kind === "number") {
        if (typeof tv === "number" && tv > 0) (next as unknown as Record<string, unknown>)[row.key] = tv;
      } else if (typeof tv === "string" && tv.trim() !== "") {
        (next as unknown as Record<string, unknown>)[row.key] = tv;
      }
    }
    saveFields.value = next;
    // TMDB 有演员时展示 TMDB 演员；没有时回退本地演员（E4）。
    if ((result.tmdb.actors || []).length) {
      castSelection.value = (result.tmdb.actors || []).map((a) => ({ ...a }));
    } else {
      castSelection.value = (result.local.actors || []).map((a) => ({ ...a, selected: true }));
    }
    episodeEdits.value = {};
    episodeStills.value = {};
    episodeStillsLoading.value = {};
    openEpisodeKey.value = "";
    selectedSeasons.value = new Set((result.seasons || []).map((s) => s.number));
    mediaInfo.value = null;
    mediaInfoError.value = "";
    rematchOpen.value = false;
    rematchCandidates.value = [];
    rematchQuery.value = result.item.title || "";
    selectedCandidateKey.value = "";
    imageTab.value = {};
    imageExpanded.value = {};
  } catch (e) {
    loadError.value = getApiErrorMessage(e, "加载工作台失败");
  } finally {
    loading.value = false;
  }
}

async function reloadAfterSave() {
  emit("saved");
  await loadWorkbench();
}

async function saveRootFields() {
  if (!props.taskId || !props.item) return;
  const changed = changedFields();
  if (!Object.keys(changed).length) {
    toast.error("没有需要保存的字段改动");
    return;
  }
  savingFields.value = true;
  try {
    await saveStrmScrapeWorkbenchFields({
      strm_task_id: props.taskId,
      item_id: props.item.id,
      kind: "work",
      fields: changed,
    });
    toast.success("基础信息已保存");
    await reloadAfterSave();
  } catch (e) {
    toast.error(getApiErrorMessage(e, "保存失败"));
  } finally {
    savingFields.value = false;
  }
}

async function setImage(kind: ImageKind, tmdbPath: string, season?: number, episode?: number) {
  if (!props.taskId || !props.item || !tmdbPath) return;
  const key = `${kind}:${tmdbPath}`;
  settingImageKey.value = key;
  try {
    await setStrmScrapeWorkbenchImage({
      strm_task_id: props.taskId,
      item_id: props.item.id,
      kind,
      tmdb_path: tmdbPath,
      season,
      episode,
    });
    toast.success("图片已设置");
    await reloadAfterSave();
  } catch (e) {
    toast.error(getApiErrorMessage(e, "设置图片失败"));
  } finally {
    settingImageKey.value = "";
  }
}

function triggerUpload(kind: ImageKind, season?: number, episode?: number) {
  uploadContext.value = { kind, season, episode };
  const input = uploadInput.value;
  if (!input) return;
  input.value = "";
  input.accept = "image/jpeg,image/png,image/webp";
  input.click();
}

async function onUploadFileSelected(ev: Event) {
  const file = (ev.target as HTMLInputElement).files?.[0];
  const ctx = uploadContext.value;
  if (!file || !ctx || !props.taskId || !props.item) return;
  if (file.size > 15 * 1024 * 1024) {
    toast.error("图片不能超过 15MB");
    return;
  }
  const key = `${ctx.kind}:${ctx.season ?? ""}:${ctx.episode ?? ""}`;
  uploadingImageKey.value = key;
  try {
    const dataUri = await new Promise<string>((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result));
      reader.onerror = () => reject(new Error("读取图片失败"));
      reader.readAsDataURL(file);
    });
    await uploadStrmScrapeWorkbenchImage({
      strm_task_id: props.taskId,
      item_id: props.item.id,
      kind: ctx.kind,
      data: dataUri,
      season: ctx.season,
      episode: ctx.episode,
    });
    toast.success("图片已上传");
    await reloadAfterSave();
  } catch (e) {
    toast.error(getApiErrorMessage(e, "上传图片失败"));
  } finally {
    uploadingImageKey.value = "";
  }
}

async function deleteImage(kind: "poster" | "backdrop" | "logo" | "season_poster", season?: number) {
  if (!props.taskId || !props.item) return;
  try {
    await confirm({
      title: "删除图片",
      message: "确定删除这张本地图片吗？",
      icon: "warning",
      confirmText: "删除",
      danger: true,
    });
  } catch {
    return;
  }
  const key = `${kind}:${season ?? ""}`;
  deletingImageKey.value = key;
  try {
    await deleteStrmScrapeWorkbenchImage({
      strm_task_id: props.taskId,
      item_id: props.item.id,
      kind,
      season,
    });
    toast.success("图片已删除");
    await reloadAfterSave();
  } catch (e) {
    toast.error(getApiErrorMessage(e, "删除图片失败"));
  } finally {
    deletingImageKey.value = "";
  }
}

async function copyMainPosterToSeason(season: number) {
  if (!props.taskId || !props.item) return;
  copyingSeason.value = season;
  try {
    await copyStrmScrapeWorkbenchImage({
      strm_task_id: props.taskId,
      item_id: props.item.id,
      from: "poster",
      to: "season_poster",
      season,
    });
    toast.success("已将主海报同步为季海报");
    await reloadAfterSave();
  } catch (e) {
    toast.error(getApiErrorMessage(e, "同步失败"));
  } finally {
    copyingSeason.value = -1;
  }
}

function addActor() {
  const name = newActorName.value.trim();
  if (!name) return;
  castSelection.value.push({
    name,
    role: "",
    order: castSelection.value.length,
    selected: true,
  });
  newActorName.value = "";
}

async function saveCast() {
  if (!props.taskId || !props.item) return;
  const actors = castSelection.value.filter((a) => a.selected);
  if (!actors.length) {
    toast.error("没有勾选任何演员");
    return;
  }
  savingCast.value = true;
  try {
    await saveStrmScrapeWorkbenchCast({
      strm_task_id: props.taskId,
      item_id: props.item.id,
      actors,
    });
    toast.success("演员已保存");
    await reloadAfterSave();
  } catch (e) {
    toast.error(getApiErrorMessage(e, "保存演员失败"));
  } finally {
    savingCast.value = false;
  }
}

async function saveEpisode(ep: StrmScrapeWorkbenchEpisode) {
  if (!props.taskId || !props.item) return;
  const edit = episodeEdit(ep);
  try {
    await saveStrmScrapeWorkbenchFields({
      strm_task_id: props.taskId,
      item_id: props.item.id,
      kind: "episode",
      season: ep.season,
      episode: ep.episode,
      fields: {},
      episode_fields: edit,
    });
    toast.success(`S${ep.season}E${ep.episode} 已保存`);
    await reloadAfterSave();
  } catch (e) {
    toast.error(getApiErrorMessage(e, "保存分集失败"));
  }
}

async function loadEpisodeStills(ep: StrmScrapeWorkbenchEpisode) {
  if (!props.taskId || !props.item) return;
  const key = episodeKey(ep.season, ep.episode);
  if (episodeStills.value[key] || episodeStillsLoading.value[key]) return;
  episodeStillsLoading.value[key] = true;
  try {
    const result = await fetchStrmScrapeWorkbenchEpisodeImages(
      props.taskId,
      props.item.id,
      ep.season,
      ep.episode,
    );
    episodeStills.value[key] = result.stills || [];
    if (!episodeStills.value[key].length) toast.error("TMDB 没有提供当前集的剧照");
  } catch (e) {
    toast.error(getApiErrorMessage(e, "读取剧照失败"));
  } finally {
    episodeStillsLoading.value[key] = false;
  }
}

async function pickEpisodeStill(ep: StrmScrapeWorkbenchEpisode, still: StrmScrapeWorkbenchImage) {
  episodeEdit(ep).thumb = still.url;
  await setImage("episode_thumb", still.file_path, ep.season, ep.episode);
}

function toggleSeason(n: number) {
  const next = new Set(selectedSeasons.value);
  if (next.has(n)) next.delete(n);
  else next.add(n);
  selectedSeasons.value = next;
}

async function saveSeries() {
  if (!props.taskId || !props.item || !data.value) return;
  const seasons = [...selectedSeasons.value].sort((a, b) => a - b);
  if (!seasons.length) {
    toast.error("请至少勾选一个季度");
    return;
  }
  try {
    await confirm({
      title: "整剧按 TMDB 保存",
      message: `将按 TMDB 更新「${data.value.item.title}」的剧集 NFO 与演员，并为已勾选的季度写入分集 NFO 与剧照（不含季海报），手动改过的分集也会被覆盖。确定继续？`,
      icon: "warning",
      confirmText: "整剧保存",
      danger: true,
    });
  } catch {
    return;
  }
  savingSeries.value = true;
  try {
    await saveStrmScrapeWorkbenchSeries({
      strm_task_id: props.taskId,
      item_id: props.item.id,
      tmdb_id: data.value.tmdb_id,
      seasons,
    });
    toast.success("整剧已按 TMDB 保存");
    await reloadAfterSave();
  } catch (e) {
    toast.error(getApiErrorMessage(e, "整剧保存失败"));
  } finally {
    savingSeries.value = false;
  }
}

async function loadMediaInfo() {
  if (!props.taskId || !props.item) return;
  mediaInfoLoading.value = true;
  mediaInfoError.value = "";
  try {
    mediaInfo.value = await fetchStrmScrapeWorkbenchMediaInfo(props.taskId, props.item.id);
  } catch (e) {
    mediaInfoError.value = getApiErrorMessage(e, "读取视频参数失败");
  } finally {
    mediaInfoLoading.value = false;
  }
}

async function saveMediaInfo() {
  if (!props.taskId || !props.item) return;
  savingMediaInfo.value = true;
  try {
    mediaInfo.value = await saveStrmScrapeWorkbenchMediaInfo(props.taskId, props.item.id);
    toast.success("视频参数已保存");
    await reloadAfterSave();
  } catch (e) {
    toast.error(getApiErrorMessage(e, "保存视频参数失败"));
  } finally {
    savingMediaInfo.value = false;
  }
}

function mediaRate(value?: string): string {
  if (!value || value === "0/0") return "未提供";
  const parts = String(value).split("/");
  const n = Number(parts[0]);
  const d = Number(parts[1] || 1);
  if (Number.isFinite(n) && Number.isFinite(d) && d) return String(n / d);
  return value;
}

function mediaDuration(value?: string): string {
  const n = Number(value);
  if (!Number.isFinite(n)) return "未提供";
  const h = Math.floor(n / 3600);
  const m = Math.floor((n % 3600) / 60);
  const s = Math.floor(n % 60);
  return (h ? `${h}小时 ` : "") + (m ? `${m}分 ` : "") + `${s}秒`;
}

function mediaBytes(value?: number): string {
  const n = Number(value);
  if (!Number.isFinite(n)) return "未提供";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(i ? 1 : 0)} ${units[i]}`;
}

function mediaValue(value: string | number | undefined | null, suffix = ""): string {
  if (value === undefined || value === null || value === "") return "未提供";
  return `${value}${suffix}`;
}

async function searchRematch() {
  if (!props.item || !data.value) return;
  const q = rematchQuery.value.trim() || data.value.item.title;
  if (!q) {
    toast.error("请输入片名或 TMDB ID");
    return;
  }
  rematchSearching.value = true;
  try {
    const results = await searchMediaOrganizeTmdb({
      query: q,
      media_type: data.value.media_type === "tv" || data.value.media_type === "movie" ? data.value.media_type : "auto",
    });
    rematchCandidates.value = Array.isArray(results) ? results.slice(0, 20) : [];
    selectedCandidateKey.value = rematchCandidates.value[0] ? hitKey(rematchCandidates.value[0]) : "";
    if (!rematchCandidates.value.length) toast.error("没有找到候选");
  } catch (e) {
    toast.error(getApiErrorMessage(e, "搜索失败"));
  } finally {
    rematchSearching.value = false;
  }
}

async function applyRematchCandidate(hit: MediaOrganizeTmdbSearchHit) {
  if (!props.taskId || !props.item || !data.value) return;
  const tmdbId = hitId(hit);
  if (!tmdbId) return;
  rematchApplying.value = true;
  try {
    await matchStrmScrapeWorkbench({
      strm_task_id: props.taskId,
      item_id: props.item.id,
      tmdb_id: tmdbId,
    });
    toast.success(`匹配已保存：TMDB ${tmdbId}`);
    rematchCandidates.value = [];
    rematchOpen.value = false;
    await loadWorkbench(tmdbId);
  } catch (e) {
    toast.error(getApiErrorMessage(e, "匹配失败"));
  } finally {
    rematchApplying.value = false;
  }
}

watch(
  () => [props.open, props.item?.id],
  () => {
    void loadWorkbench();
  },
  { immediate: true },
);
</script>

<template>
  <AppModal :open="open" size="lg" @close="emit('close')">
    <template #title>精修工作台</template>
    <template #actions>
      <AppButton size="sm" variant="ghost" @click="emit('close')">关闭</AppButton>
    </template>

    <div v-if="loading && !data" class="wb-loading">
      <BusySpinner />
      <span>加载工作台…</span>
    </div>
    <div v-else-if="loadError" class="wb-error">
      <p>{{ loadError }}</p>
      <AppButton size="sm" @click="loadWorkbench()">重新加载</AppButton>
    </div>
    <div v-else-if="data" class="wb">
      <div class="wb-hero">
        <div class="wb-hero__bg" :style="heroBgStyle" />
        <div class="wb-hero__ov" />
        <div class="wb-hero__content">
          <div class="wb-hero__poster">
            <img v-if="heroPosterUrl" :src="heroPosterUrl" alt="" />
          </div>
          <div class="wb-hero__text">
            <img v-if="heroLogoUrl" :src="heroLogoUrl" class="wb-hero__logo" :alt="data.item.title" />
            <h2 v-else class="wb-hero__title">{{ data.item.title }}</h2>
            <div class="wb-hero__meta">{{ heroMeta }}</div>
          </div>
        </div>
      </div>

      <section class="wb-section">
        <div class="wb-section__head">
          <h3>基础信息</h3>
          <div class="wb-section__actions">
            <AppButton size="sm" variant="ghost" :disabled="!data.tmdb_available" @click="applyAllTmdb">
              全部采用 TMDB
            </AppButton>
            <AppButton size="sm" variant="ghost" @click="rematchOpen = !rematchOpen">
              {{ rematchOpen ? "取消匹配" : "匹配 TMDB" }}
            </AppButton>
            <AppButton size="sm" :loading="savingFields" @click="saveRootFields">保存到 NFO</AppButton>
          </div>
        </div>
        <p v-if="!data.tmdb_available && data.tmdb_error" class="wb-hint">{{ data.tmdb_error }}</p>
        <p v-if="data.credits_error" class="wb-hint wb-hint--error">{{ data.credits_error }}</p>
        <p v-if="data.images_error" class="wb-hint wb-hint--error">{{ data.images_error }}</p>

        <div v-if="rematchOpen" class="wb-rematch">
          <input
            v-model="rematchQuery"
            type="text"
            class="wb-input"
            placeholder="输入片名或 TMDB ID"
            @keyup.enter="searchRematch"
          />
          <AppButton size="sm" :loading="rematchSearching" @click="searchRematch">搜索</AppButton>
          <div v-if="rematchCandidates.length" class="wb-rematch__list">
            <button
              v-for="hit in rematchCandidates"
              :key="hitKey(hit)"
              class="wb-rematch__item"
              :class="{ 'is-selected': selectedCandidateKey === hitKey(hit) }"
              @click="selectedCandidateKey = hitKey(hit)"
            >
              <img v-if="hitPosterURL(hit, 'w154')" :src="hitPosterURL(hit, 'w154')" alt="" class="wb-rematch__poster" />
              <span class="wb-rematch__title">{{ hitTitle(hit) }}</span>
              <span class="wb-rematch__meta">{{ hitYear(hit) || "" }}</span>
            </button>
            <AppButton
              size="sm"
              :loading="rematchApplying"
              :disabled="!selectedCandidateKey"
              @click="applyRematchCandidate(rematchCandidates.find((h) => hitKey(h) === selectedCandidateKey)!)"
            >
              确认匹配
            </AppButton>
          </div>
        </div>

        <table class="wb-table">
          <thead>
            <tr>
              <th>字段</th>
              <th>本地</th>
              <th>TMDB</th>
              <th>保存值</th>
              <th class="wb-table__actions">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in FIELD_ROWS" :key="row.key">
              <td>{{ row.label }}</td>
              <td class="wb-table__cell">{{ fieldText(row.key, "local") || "—" }}</td>
              <td class="wb-table__cell">{{ fieldText(row.key, "tmdb") || "—" }}</td>
              <td>
                <textarea
                  v-if="row.kind === 'textarea'"
                  class="wb-input wb-input--textarea"
                  rows="2"
                  :value="fieldText(row.key, 'save')"
                  @input="setFieldValue(row.key, ($event.target as HTMLTextAreaElement).value)"
                />
                <input
                  v-else
                  class="wb-input"
                  :type="row.kind === 'number' ? 'number' : 'text'"
                  :value="fieldText(row.key, 'save')"
                  @input="setFieldValue(row.key, ($event.target as HTMLInputElement).value)"
                />
              </td>
              <td>
                <AppButton
                  size="sm"
                  variant="ghost"
                  :disabled="!data.tmdb_available || tmdbFieldEmpty(row.key)"
                  @click="applyTmdbField(row.key)"
                >
                  采用
                </AppButton>
              </td>
            </tr>
          </tbody>
        </table>
      </section>

      <section class="wb-section">
        <h3>图片</h3>
        <div v-for="pick in IMAGE_PICKERS" :key="pick.kind" class="wb-images">
          <h4>{{ pick.label }}</h4>
          <div class="wb-images__local">
            <span class="wb-images__local-label">本地当前：</span>
            <img
              v-if="localImage(pick.kind)?.exists && localImage(pick.kind)?.url"
              :src="localImage(pick.kind)!.url"
              :alt="pick.label"
              class="wb-images__local-img"
            />
            <span v-else class="wb-empty">无</span>
            <AppButton size="sm" variant="ghost" @click="triggerUpload(pick.kind)">上传本地图</AppButton>
            <AppButton
              v-if="localImage(pick.kind)?.exists"
              size="sm"
              variant="ghost"
              :loading="deletingImageKey === `${pick.kind}:`"
              @click="deleteImage(pick.kind)"
            >
              删除
            </AppButton>
          </div>
          <template v-if="groupImages(tmdbImages(pick.kind)).length">
            <div class="wb-lang-tabs">
              <button
                v-for="(g, gi) in groupImages(tmdbImages(pick.kind))"
                :key="g.key"
                class="wb-lang-tab"
                :class="{ 'is-active': (imageTab[pick.kind] || 0) === gi }"
                @click="imageTab[pick.kind] = gi"
              >
                {{ g.label }} · {{ g.items.length }}
              </button>
            </div>
            <div class="wb-images__grid">
              <button
                v-for="img in visibleGroupImages(pick.kind, groupImages(tmdbImages(pick.kind)))"
                :key="img.url"
                class="wb-images__card"
                :disabled="settingImageKey === `${pick.kind}:${img.file_path}`"
                @click="setImage(pick.kind, img.file_path)"
              >
                <img :src="previewUrl(img, pick.kind)" :alt="pick.label" loading="lazy" />
                <span class="wb-images__meta">
                  {{ img.width }}×{{ img.height }} · {{ languageLabel(languageGroupKey(img.language)) }}
                </span>
              </button>
            </div>
            <AppButton
              v-if="groupRestCount(pick.kind, groupImages(tmdbImages(pick.kind))) > 0 || groupExpanded(pick.kind, groupImages(tmdbImages(pick.kind)))"
              size="sm"
              variant="ghost"
              @click="toggleGroupExpand(pick.kind, groupImages(tmdbImages(pick.kind)))"
            >
              {{
                groupExpanded(pick.kind, groupImages(tmdbImages(pick.kind)))
                  ? "收起"
                  : `展开剩余 ${groupRestCount(pick.kind, groupImages(tmdbImages(pick.kind)))} 张`
              }}
            </AppButton>
          </template>
          <p v-else class="wb-images__empty">TMDB 无候选图</p>
        </div>

        <div v-if="data.media_type === 'tv' && (data.seasons || []).length" class="wb-season-posters">
          <h4>季海报</h4>
          <div v-for="season in data.seasons" :key="season.number" class="wb-season-poster">
            <div class="wb-season-poster__head">
              <span>第 {{ season.number }} 季</span>
              <span v-if="season.name" class="wb-empty">{{ season.name }}</span>
            </div>
            <div class="wb-images__local">
              <span class="wb-images__local-label">本地当前：</span>
              <img v-if="season.poster_url" :src="season.poster_url" alt="季海报" class="wb-images__local-img" />
              <span v-else class="wb-empty">无</span>
              <AppButton
                size="sm"
                variant="ghost"
                :loading="copyingSeason === season.number"
                :disabled="!localImage('poster')?.exists"
                @click="copyMainPosterToSeason(season.number)"
              >
                使用主海报
              </AppButton>
              <AppButton size="sm" variant="ghost" @click="triggerUpload('season_poster', season.number)">
                上传
              </AppButton>
              <AppButton
                v-if="season.poster_url"
                size="sm"
                variant="ghost"
                :loading="deletingImageKey === `season_poster:${season.number}`"
                @click="deleteImage('season_poster', season.number)"
              >
                删除
              </AppButton>
            </div>
            <template v-if="groupImages(season.posters || []).length">
              <div class="wb-lang-tabs">
                <button
                  v-for="(g, gi) in groupImages(season.posters || [])"
                  :key="g.key"
                  class="wb-lang-tab"
                  :class="{ 'is-active': (imageTab[`season_${season.number}`] || 0) === gi }"
                  @click="imageTab[`season_${season.number}`] = gi"
                >
                  {{ g.label }} · {{ g.items.length }}
                </button>
              </div>
              <div class="wb-images__grid">
                <button
                  v-for="img in visibleGroupImages(`season_${season.number}`, groupImages(season.posters || []))"
                  :key="img.url"
                  class="wb-images__card"
                  :disabled="settingImageKey === `season_poster:${img.file_path}`"
                  @click="setImage('season_poster', img.file_path, season.number)"
                >
                  <img :src="img.url" alt="季海报" loading="lazy" />
                  <span class="wb-images__meta">
                    {{ img.width }}×{{ img.height }} · {{ languageLabel(languageGroupKey(img.language)) }}
                  </span>
                </button>
              </div>
              <AppButton
                v-if="groupRestCount(`season_${season.number}`, groupImages(season.posters || [])) > 0 || groupExpanded(`season_${season.number}`, groupImages(season.posters || []))"
                size="sm"
                variant="ghost"
                @click="toggleGroupExpand(`season_${season.number}`, groupImages(season.posters || []))"
              >
                {{
                  groupExpanded(`season_${season.number}`, groupImages(season.posters || []))
                    ? "收起"
                    : `展开剩余 ${groupRestCount(`season_${season.number}`, groupImages(season.posters || []))} 张`
                }}
              </AppButton>
            </template>
            <p v-else class="wb-images__empty">TMDB 无候选图</p>
          </div>
        </div>
      </section>

      <section class="wb-section">
        <div class="wb-section__head">
          <h3>演员</h3>
          <AppButton size="sm" :loading="savingCast" :disabled="!castSelection.length" @click="saveCast">
            保存演员
          </AppButton>
        </div>
        <div class="wb-cast__add">
          <input v-model="newActorName" type="text" class="wb-input" placeholder="手动添加演员姓名" />
          <AppButton size="sm" variant="ghost" :disabled="!newActorName.trim()" @click="addActor">添加演员</AppButton>
        </div>
        <div v-if="castSelection.length" class="wb-cast">
          <label v-for="actor in castSelection" :key="actor.name" class="wb-cast__row">
            <input v-model="actor.selected" type="checkbox" />
            <img v-if="actor.thumb_url" :src="actor.thumb_url" alt="" class="wb-cast__thumb" loading="lazy" />
            <span class="wb-cast__name">{{ actor.name }}</span>
            <input
              v-model="actor.role"
              type="text"
              class="wb-input wb-cast__role"
              placeholder="角色（可编辑）"
            />
          </label>
        </div>
        <p v-else class="wb-empty">TMDB 与本地均无演员</p>
      </section>

      <section v-if="data.media_type === 'tv'" class="wb-section">
        <h3>季与分集</h3>
        <div v-for="season in data.seasons" :key="season.number" class="wb-season">
          <div class="wb-season__head">
            <label class="wb-season__check">
              <input
                type="checkbox"
                :checked="selectedSeasons.has(season.number)"
                @change="toggleSeason(season.number)"
              />
              <strong>第 {{ season.number }} 季</strong>
              <span v-if="season.name" class="wb-empty">{{ season.name }}</span>
            </label>
            <span class="wb-season__meta">{{ season.episodes?.length || 0 }} 集</span>
          </div>
          <div v-for="ep in season.episodes" :key="`${ep.season}-${ep.episode}`" class="wb-episode">
            <button class="wb-episode__head" @click="toggleEpisode(ep.season, ep.episode)">
              <span>
                S{{ ep.season }}E{{ ep.episode }} · {{ ep.title || "未命名" }}
              </span>
              <span class="wb-episode__meta">
                {{ ep.air_date || "" }}
                <span v-if="!ep.local_nfo" class="wb-episode__tag">未建 NFO</span>
                <span v-if="!ep.local_thumb" class="wb-episode__tag">无剧照</span>
              </span>
              <span>{{ openEpisodeKey === `${ep.season}-${ep.episode}` ? "▾" : "▸" }}</span>
            </button>
            <div v-if="openEpisodeKey === `${ep.season}-${ep.episode}`" class="wb-episode__body">
              <label class="wb-episode__field">
                标题
                <input v-model="episodeEdit(ep).title" class="wb-input" type="text" />
              </label>
              <label class="wb-episode__field">
                简介
                <textarea v-model="episodeEdit(ep).plot" class="wb-input wb-input--textarea" rows="3" />
              </label>
              <div class="wb-episode__row">
                <label class="wb-episode__field">
                  播出日期
                  <input v-model="episodeEdit(ep).aired" class="wb-input" type="text" placeholder="YYYY-MM-DD" />
                </label>
                <label class="wb-episode__field">
                  时长（分钟）
                  <input v-model.number="episodeEdit(ep).runtime" class="wb-input" type="number" min="0" />
                </label>
              </div>
              <div class="wb-episode__field">
                剧照
                <div class="wb-episode__stills">
                  <img
                    v-if="ep.local_thumb_url"
                    :src="ep.local_thumb_url"
                    alt="本地剧照"
                    class="wb-episode__still"
                  />
                  <img
                    v-else-if="ep.still_url"
                    :src="ep.still_url"
                    alt="TMDB 剧照"
                    class="wb-episode__still"
                  />
                  <span v-else class="wb-empty">无剧照</span>
                </div>
                <div class="wb-episode__still-actions">
                  <AppButton size="sm" variant="ghost" @click="triggerUpload('episode_thumb', ep.season, ep.episode)">
                    上传本地剧照
                  </AppButton>
                  <AppButton
                    size="sm"
                    variant="ghost"
                    :loading="episodeStillsLoading[`${ep.season}-${ep.episode}`]"
                    @click="loadEpisodeStills(ep)"
                  >
                    读取 TMDB 剧照
                  </AppButton>
                </div>
                <div
                  v-if="episodeStills[`${ep.season}-${ep.episode}`]?.length"
                  class="wb-images__grid wb-episode__still-grid"
                >
                  <button
                    v-for="still in episodeStills[`${ep.season}-${ep.episode}`]"
                    :key="still.url"
                    class="wb-images__card"
                    :disabled="settingImageKey === `episode_thumb:${still.file_path}`"
                    @click="pickEpisodeStill(ep, still)"
                  >
                    <img :src="still.url" alt="剧照" loading="lazy" />
                    <span class="wb-images__meta">
                      {{ still.width }}×{{ still.height }} · {{ languageLabel(languageGroupKey(still.language)) }}
                    </span>
                  </button>
                </div>
              </div>
              <div class="wb-episode__actions">
                <AppButton size="sm" @click="saveEpisode(ep)">保存本集</AppButton>
              </div>
            </div>
          </div>
        </div>
        <p v-if="!(data.seasons || []).length" class="wb-empty">无季信息</p>
      </section>

      <section class="wb-section">
        <div class="wb-section__head">
          <h3>视频信息</h3>
          <div class="wb-section__actions">
            <AppButton size="sm" variant="ghost" :loading="mediaInfoLoading" @click="loadMediaInfo">
              读取视频参数
            </AppButton>
            <AppButton
              size="sm"
              :loading="savingMediaInfo"
              :disabled="!mediaInfo"
              @click="saveMediaInfo"
            >
              保存到 NFO
            </AppButton>
          </div>
        </div>
        <p v-if="mediaInfoError" class="wb-hint wb-hint--error">{{ mediaInfoError }}</p>
        <div v-if="mediaInfo" class="wb-media">
          <div class="wb-media__grid">
            <div><span>文件</span><strong>{{ mediaInfo.filename || "—" }}</strong></div>
            <div><span>来源</span><strong>{{ mediaInfo.source || "—" }}</strong></div>
            <div><span>容器</span><strong>{{ mediaInfo.format || "—" }}</strong></div>
            <div><span>时长</span><strong>{{ mediaDuration(mediaInfo.duration) }}</strong></div>
            <div><span>码率</span><strong>{{ mediaValue(mediaInfo.bit_rate) }}</strong></div>
            <div><span>大小</span><strong>{{ mediaBytes(mediaInfo.file_size) }}</strong></div>
          </div>
          <div v-for="(v, i) in mediaInfo.video" :key="i" class="wb-media__stream">
            <strong>视频 {{ i + 1 }}</strong>
            <span>编码 {{ mediaValue(v.codec) }}</span>
            <span v-if="v.width && v.height">分辨率 {{ v.width }}×{{ v.height }}</span>
            <span v-if="v.fps">帧率 {{ mediaRate(v.fps) }}</span>
            <span v-if="v.pix_fmt">像素格式 {{ v.pix_fmt }}</span>
            <span v-if="v.hdr">HDR {{ v.hdr }}</span>
            <span v-if="v.bit_rate">码率 {{ mediaValue(v.bit_rate) }}</span>
          </div>
          <div v-for="(a, i) in mediaInfo.audio" :key="i" class="wb-media__stream">
            <strong>音频 {{ i + 1 }}</strong>
            <span>编码 {{ mediaValue(a.codec) }}</span>
            <span v-if="a.channels">声道 {{ a.channels }}</span>
            <span v-if="a.language">语言 {{ a.language }}</span>
            <span v-if="a.sample_rate">采样率 {{ mediaValue(a.sample_rate) }}</span>
            <span v-if="a.bit_rate">码率 {{ mediaValue(a.bit_rate) }}</span>
          </div>
          <div v-for="(s, i) in mediaInfo.subtitles" :key="i" class="wb-media__stream">
            <strong>字幕 {{ i + 1 }}</strong>
            <span>编码 {{ mediaValue(s.codec) }}</span>
            <span v-if="s.language">语言 {{ s.language }}</span>
            <span v-if="s.title">标题 {{ s.title }}</span>
          </div>
          <p v-if="!mediaInfo.video.length && !mediaInfo.audio.length && !mediaInfo.subtitles.length" class="wb-empty">
            未检测到音视频流
          </p>
        </div>
        <p v-else-if="!mediaInfoLoading" class="wb-empty">未读取视频参数</p>
      </section>

      <section class="wb-section">
        <h3>NFO 预览（仅改动字段与演员）</h3>
        <pre class="wb-preview">{{ nfoPreview }}</pre>
      </section>

      <section v-if="data.media_type === 'tv'" class="wb-section wb-series">
        <h3>整剧保存</h3>
        <p class="wb-hint">
          按 TMDB 更新剧集 NFO 与演员，并为上方已勾选的季度写入分集 NFO 与剧照（不含季海报）。
          手动改过的分集也会被覆盖。
        </p>
        <AppButton size="sm" variant="primary" :loading="savingSeries" @click="saveSeries">
          整剧按 TMDB 保存
        </AppButton>
      </section>
    </div>

    <input ref="uploadInput" type="file" class="wb-upload-input" @change="onUploadFileSelected" />
  </AppModal>
</template>

<style scoped>
.wb-loading,
.wb-error {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  padding: 48px 16px;
  color: var(--text-muted);
}

.wb {
  display: flex;
  flex-direction: column;
  gap: 20px;
  padding: 4px 0 8px;
}

/* 顶部 hero（media-editor 媒体墙同款结构，mono 中性配色）：
   负边距抵掉 AppModal body 的 20px 内边距与 .wb 的 4px 上内边距，
   让整面墙贴到弹窗边缘；图片区深浅模式长一样，不分主题。 */
.wb-hero {
  position: relative;
  min-height: 440px;
  display: flex;
  align-items: flex-end;
  padding: 32px;
  margin: -24px -20px 0;
  overflow: hidden;
  color: #fff;
  background: #1a1a1a;
  border-bottom: 1px solid var(--border);
}

.wb-hero__bg {
  position: absolute;
  inset: 0;
  background-size: 100% auto;
  background-repeat: no-repeat;
  background-position: top center;
  opacity: 0.9;
}

.wb-hero__ov {
  position: absolute;
  inset: 0;
  background:
    linear-gradient(0deg, #1a1a1a 0%, rgba(26, 26, 26, 0) 45%),
    linear-gradient(180deg, rgba(0, 0, 0, 0.45) 0%, rgba(0, 0, 0, 0) 30%);
}

.wb-hero__content {
  position: relative;
  display: flex;
  gap: 24px;
  align-items: flex-end;
  min-width: 0;
}

.wb-hero__poster {
  width: 168px;
  flex: none;
  aspect-ratio: 2 / 3;
  overflow: hidden;
  border: 1px solid rgba(255, 255, 255, 0.16);
  border-radius: var(--radius-sm);
  background: #2c2c2e;
}

.wb-hero__poster img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.wb-hero__text {
  min-width: 0;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 8px;
}

.wb-hero__logo {
  display: block;
  max-width: 320px;
  max-height: 72px;
  width: auto;
  height: auto;
  object-fit: contain;
  filter: drop-shadow(0 2px 6px rgba(0, 0, 0, 0.5));
}

.wb-hero__title {
  margin: 0;
  font-size: 30px;
  font-weight: 700;
  line-height: 1.2;
  color: #fff;
  text-shadow: 0 1px 8px rgba(0, 0, 0, 0.6);
  word-break: break-word;
}

.wb-hero__meta {
  display: inline-block;
  padding: 4px 10px;
  border-radius: var(--radius-xs);
  background: rgba(0, 0, 0, 0.75);
  color: #fff;
  font-size: 13px;
  font-weight: 600;
  word-break: break-all;
}

@media (max-width: 768px) {
  .wb-hero {
    min-height: 300px;
    padding: 20px;
    margin: -20px -16px 0;
  }
  .wb-hero__poster {
    width: 110px;
  }
  .wb-hero__title {
    font-size: 22px;
  }
  .wb-hero__meta {
    font-size: 12px;
  }
  .wb-hero__logo {
    max-width: 220px;
    max-height: 56px;
  }
}

.wb-section {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 14px;
  border: 1px solid var(--border);
  border-radius: var(--radius-md);
  background: var(--surface);
}

.wb-section h3 {
  margin: 0;
  font-size: 15px;
}

.wb-section h4 {
  margin: 0;
  font-size: 13px;
}

.wb-section__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.wb-section__actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.wb-hint {
  margin: 0;
  font-size: 12px;
  color: var(--text-muted);
}

.wb-hint--error {
  color: var(--danger);
}

.wb-empty {
  margin: 0;
  font-size: 12px;
  color: var(--text-muted);
}

.wb-input {
  width: 100%;
  padding: 6px 8px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg);
  color: var(--text);
  font-size: 13px;
}

.wb-input--textarea {
  resize: vertical;
}

.wb-table {
  width: 100%;
  border-collapse: collapse;
  font-size: 13px;
}

.wb-table th,
.wb-table td {
  padding: 8px;
  border-bottom: 1px solid var(--border-soft);
  text-align: left;
  vertical-align: top;
}

.wb-table th {
  color: var(--text-muted);
  font-weight: 600;
}

.wb-table__cell {
  white-space: pre-wrap;
  word-break: break-word;
  max-width: 240px;
}

.wb-table__actions {
  width: 64px;
}

.wb-rematch {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.wb-rematch__list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.wb-rematch__item {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg);
  color: var(--text);
  cursor: pointer;
  text-align: left;
}

.wb-rematch__item.is-selected {
  border-color: var(--mono-accent);
  outline: 1px solid var(--mono-accent);
}

.wb-rematch__poster {
  width: 36px;
  height: 54px;
  object-fit: cover;
  border-radius: var(--radius-xs);
  background: var(--surface-sunken);
}

.wb-rematch__title {
  flex: 1;
  font-size: 13px;
}

.wb-rematch__meta {
  font-size: 12px;
  color: var(--text-muted);
}

.wb-images {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--border-soft);
}

.wb-images:last-child {
  border-bottom: 0;
  padding-bottom: 0;
}

.wb-images__local {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  font-size: 12px;
}

.wb-images__local-label {
  color: var(--text-muted);
}

.wb-images__local-img {
  max-height: 96px;
  max-width: 160px;
  object-fit: contain;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface-sunken);
}

.wb-lang-tabs {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}

.wb-lang-tab {
  padding: 4px 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-pill);
  background: var(--bg);
  color: var(--text-muted);
  font-size: 12px;
  cursor: pointer;
}

.wb-lang-tab.is-active {
  background: var(--mono-accent);
  border-color: var(--mono-accent);
  color: var(--mono-accent-text, #fff);
}

.wb-images__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
  gap: 8px;
}

.wb-images__card {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 4px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--bg);
  color: var(--text);
  cursor: pointer;
  text-align: left;
}

.wb-images__card:disabled {
  opacity: 0.6;
  cursor: default;
}

.wb-images__card img {
  width: 100%;
  aspect-ratio: 2 / 3;
  object-fit: cover;
  border-radius: var(--radius-xs);
  background: var(--surface-sunken);
}

.wb-images__meta {
  font-size: 11px;
  color: var(--text-muted);
}

.wb-images__empty {
  margin: 0;
  font-size: 12px;
  color: var(--text-muted);
}

.wb-season-posters {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding-top: 12px;
  border-top: 1px solid var(--border-soft);
}

.wb-season-poster {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--border-soft);
}

.wb-season-poster:last-child {
  border-bottom: 0;
  padding-bottom: 0;
}

.wb-season-poster__head {
  display: flex;
  gap: 8px;
  align-items: center;
  font-size: 13px;
  font-weight: 600;
}

.wb-cast__add {
  display: flex;
  gap: 8px;
}

.wb-cast {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.wb-cast__row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 6px 8px;
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-sm);
  background: var(--bg);
}

.wb-cast__thumb {
  width: 32px;
  height: 32px;
  object-fit: cover;
  border-radius: 50%;
  background: var(--surface-sunken);
}

.wb-cast__name {
  min-width: 80px;
  font-size: 13px;
}

.wb-cast__role {
  flex: 1;
}

.wb-season {
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-sm);
  overflow: hidden;
}

.wb-season__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 10px 12px;
  background: var(--surface-muted);
}

.wb-season__check {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  font-size: 13px;
}

.wb-season__meta {
  font-size: 12px;
  color: var(--text-muted);
}

.wb-episode {
  border-top: 1px solid var(--border-soft);
}

.wb-episode__head {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 10px 12px;
  border: 0;
  background: transparent;
  color: var(--text);
  font-size: 13px;
  cursor: pointer;
  text-align: left;
}

.wb-episode__head > span:first-child {
  flex: 1;
}

.wb-episode__meta {
  font-size: 12px;
  color: var(--text-muted);
}

.wb-episode__tag {
  margin-left: 6px;
  padding: 1px 6px;
  border: 1px solid var(--warning);
  border-radius: var(--radius-pill);
  color: var(--warning);
}

.wb-episode__body {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 0 12px 12px;
}

.wb-episode__field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  font-size: 12px;
  color: var(--text-muted);
}

.wb-episode__row {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 10px;
}

.wb-episode__stills {
  display: flex;
  gap: 8px;
  align-items: center;
}

.wb-episode__still {
  max-height: 110px;
  max-width: 220px;
  object-fit: contain;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface-sunken);
}

.wb-episode__still-actions {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}

.wb-episode__still-grid {
  margin-top: 8px;
}

.wb-episode__still-grid .wb-images__card img {
  aspect-ratio: 16 / 9;
}

.wb-episode__actions {
  display: flex;
  justify-content: flex-end;
}

.wb-media {
  display: flex;
  flex-direction: column;
  gap: 10px;
  font-size: 13px;
}

.wb-media__grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 8px;
}

.wb-media__grid > div {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 8px;
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-sm);
  background: var(--bg);
}

.wb-media__grid span {
  font-size: 12px;
  color: var(--text-muted);
}

.wb-media__stream {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  align-items: center;
  padding: 8px;
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-sm);
  background: var(--bg);
  font-size: 12px;
}

.wb-preview {
  margin: 0;
  padding: 12px;
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-sm);
  background: var(--surface-sunken);
  color: var(--text);
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-word;
}

.wb-series {
  align-items: flex-start;
}

.wb-upload-input {
  display: none;
}
</style>
