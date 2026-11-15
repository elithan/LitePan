<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { getApiErrorMessage } from "@/api/client";
import {
  fetchStrmScrapeWorkbench,
  saveStrmScrapeWorkbenchCast,
  saveStrmScrapeWorkbenchFields,
  saveStrmScrapeWorkbenchSeries,
  setStrmScrapeWorkbenchImage,
  type StrmScrapeItem,
  type StrmScrapeWorkbenchActor,
  type StrmScrapeWorkbenchData,
  type StrmScrapeWorkbenchEpisode,
  type StrmScrapeWorkbenchEpisodeFields,
  type StrmScrapeWorkbenchFieldSet,
  type StrmScrapeWorkbenchImage,
  type StrmScrapeWorkbenchLocalImage,
} from "@/api/strmScrape";
import AppButton from "@/components/base/AppButton.vue";
import AppModal from "@/components/base/AppModal.vue";
import BusySpinner from "@/components/base/BusySpinner.vue";
import { confirm } from "@/composables/useConfirm";
import { toast } from "@/composables/useToast";

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

// 与 media-editor ROOT_FIELDS 对齐的栏位表。
const FIELD_ROWS: FieldRowDef[] = [
  { key: "title", label: "标题", kind: "text" },
  { key: "originaltitle", label: "原标题", kind: "text" },
  { key: "year", label: "年份", kind: "number" },
  { key: "plot", label: "简介", kind: "textarea" },
  { key: "genre", label: "类型", kind: "list" },
  { key: "premiered", label: "首播 / 上映", kind: "text" },
  { key: "status", label: "状态", kind: "text" },
  { key: "sorttitle", label: "排序标题", kind: "text" },
  { key: "tmdbid", label: "TMDB ID", kind: "text" },
  { key: "rating", label: "评分", kind: "number" },
  { key: "votes", label: "票数", kind: "number" },
  { key: "mpaa", label: "MPAA", kind: "text" },
  { key: "country", label: "国家 / 地区", kind: "text" },
  { key: "tag", label: "标签", kind: "list" },
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
const openEpisodeKey = ref("");
const episodeEdits = ref<Record<string, StrmScrapeWorkbenchEpisodeFields>>({});

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
  if (!row || !data.value) return;
  const src = fieldsRecord(data.value.tmdb.fields)[key];
  const target = saveFields.value as unknown as Record<string, unknown>;
  if (row.kind === "list") {
    target[key] = Array.isArray(src) ? [...src] : [];
    return;
  }
  if (row.kind === "number") {
    target[key] = src === undefined || src === null ? (key === "year" ? undefined : 0) : src;
    return;
  }
  target[key] = src === undefined || src === null ? "" : src;
}

function applyAllTmdb() {
  for (const row of FIELD_ROWS) applyTmdbField(row.key);
}

function escapeXml(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

const nfoPreview = computed(() => {
  if (!data.value) return "";
  const f = saveFields.value;
  const root = data.value.media_type === "tv" ? "tvshow" : "movie";
  const lines: string[] = ['<?xml version="1.0" encoding="UTF-8"?>', `<${root}>`];
  const add = (tag: string, v: string | number | undefined) => {
    if (v === undefined || v === null || v === "" || v === 0) return;
    lines.push(`  <${tag}>${escapeXml(String(v))}</${tag}>`);
  };
  add("title", f.title);
  add("originaltitle", f.originaltitle);
  add("year", f.year);
  add("plot", f.plot);
  for (const g of f.genre || []) add("genre", g);
  add("premiered", f.premiered);
  add("status", f.status);
  add("sorttitle", f.sorttitle);
  add("tmdbid", f.tmdbid);
  if (f.tmdbid || f.uniqueid) {
    lines.push(`  <uniqueid type="tmdb">${escapeXml(f.uniqueid || f.tmdbid)}</uniqueid>`);
  }
  add("rating", f.rating);
  add("votes", f.votes);
  add("mpaa", f.mpaa);
  add("country", f.country);
  for (const t of f.tag || []) add("tag", t);
  for (const a of castSelection.value.filter((x) => x.selected)) {
    lines.push("  <actor>");
    lines.push(`    <name>${escapeXml(a.name)}</name>`);
    if (a.role) lines.push(`    <role>${escapeXml(a.role)}</role>`);
    lines.push(`    <order>${a.order}</order>`);
    if (a.thumb_url) lines.push(`    <thumb>${escapeXml(a.thumb_url)}</thumb>`);
    lines.push("  </actor>");
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

async function loadWorkbench() {
  if (!props.open || !props.item || !props.taskId) return;
  loading.value = true;
  loadError.value = "";
  try {
    const result = await fetchStrmScrapeWorkbench(props.taskId, props.item.id);
    data.value = result;
    saveFields.value = cloneFields(result.local.fields);
    castSelection.value = (result.tmdb.actors || []).map((a) => ({ ...a }));
    episodeEdits.value = {};
    openEpisodeKey.value = "";
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
  savingFields.value = true;
  try {
    await saveStrmScrapeWorkbenchFields({
      strm_task_id: props.taskId,
      item_id: props.item.id,
      kind: "work",
      fields: saveFields.value,
      episode_fields: { season: 0, episode: 0, title: "", plot: "", aired: "", thumb: "", runtime: 0 },
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

async function saveCast() {
  if (!props.taskId || !props.item) return;
  savingCast.value = true;
  try {
    await saveStrmScrapeWorkbenchCast({
      strm_task_id: props.taskId,
      item_id: props.item.id,
      actors: castSelection.value.filter((a) => a.selected),
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
      fields: saveFields.value,
      episode_fields: edit,
    });
    toast.success(`S${ep.season}E${ep.episode} 已保存`);
    await reloadAfterSave();
  } catch (e) {
    toast.error(getApiErrorMessage(e, "保存分集失败"));
  }
}

async function saveSeries() {
  if (!props.taskId || !props.item || !data.value) return;
  try {
    await confirm({
      title: "整剧按 TMDB 保存",
      message: `将按 TMDB 覆盖「${data.value.item.title}」的剧集 NFO、演员、季海报与分集 NFO / 剧照，手动改过的分集也会被覆盖。确定继续？`,
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
    });
    toast.success("整剧已按 TMDB 保存");
    await reloadAfterSave();
  } catch (e) {
    toast.error(getApiErrorMessage(e, "整剧保存失败"));
  } finally {
    savingSeries.value = false;
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
  <AppModal :open="open" :title="`精修：${item?.title || ''}`" size="branch" @close="emit('close')">
    <div v-if="loading" class="wb-loading"><BusySpinner :size="20" /> <span>加载中…</span></div>
    <div v-else-if="loadError" class="wb-error">
      <span>{{ loadError }}</span>
      <AppButton type="button" variant="secondary" @click="loadWorkbench">重试</AppButton>
    </div>
    <div v-else-if="data" class="wb">
      <p v-if="data.tmdb_error" class="wb-hint">{{ data.tmdb_error }}</p>

      <section class="wb-section">
        <div class="wb-section__head">
          <h3>基础信息</h3>
          <AppButton
            type="button"
            variant="secondary"
            :disabled="!data.tmdb_available"
            @click="applyAllTmdb"
          >
            全部采用 TMDB
          </AppButton>
        </div>
        <div class="wb-fields">
          <div class="wb-field wb-field--head">
            <span>字段</span><span>本地值</span><span>TMDB</span><span>保存值</span><span></span>
          </div>
          <div v-for="row in FIELD_ROWS" :key="row.key" class="wb-field">
            <span class="wb-field__label">{{ row.label }}</span>
            <span class="wb-field__val">{{ fieldText(row.key, "local") || "—" }}</span>
            <span class="wb-field__val wb-field__val--tmdb">{{ fieldText(row.key, "tmdb") || "—" }}</span>
            <span class="wb-field__edit">
              <textarea
                v-if="row.kind === 'textarea'"
                :value="fieldText(row.key, 'save')"
                rows="2"
                @input="setFieldValue(row.key, ($event.target as HTMLTextAreaElement).value)"
              />
              <input
                v-else
                :value="fieldText(row.key, 'save')"
                :type="row.kind === 'number' ? 'number' : 'text'"
                @input="setFieldValue(row.key, ($event.target as HTMLInputElement).value)"
              />
            </span>
            <button
              type="button"
              class="wb-field__use"
              :disabled="!data.tmdb_available || !fieldText(row.key, 'tmdb')"
              title="采用 TMDB 值"
              @click="applyTmdbField(row.key)"
            >
              采用
            </button>
          </div>
        </div>
        <pre class="wb-nfo">{{ nfoPreview }}</pre>
        <div class="wb-section__foot">
          <AppButton type="button" variant="primary" :disabled="savingFields" @click="saveRootFields">
            {{ savingFields ? "保存中…" : "保存基础信息" }}
          </AppButton>
        </div>
      </section>

      <section class="wb-section">
        <div class="wb-section__head"><h3>图片</h3></div>
        <div v-for="pick in IMAGE_PICKERS" :key="pick.kind" class="wb-images">
          <h4>{{ pick.label }}</h4>
          <div class="wb-images__local">
            <span>本地当前：</span>
            <img
              v-if="localImage(pick.kind)?.exists && localImage(pick.kind)?.url"
              :src="localImage(pick.kind)?.url"
              :alt="pick.label"
            />
            <span v-else>无</span>
          </div>
          <div class="wb-images__grid">
            <button
              v-for="img in tmdbImages(pick.kind)"
              :key="img.file_path"
              type="button"
              class="wb-images__card"
              :disabled="settingImageKey === `${pick.kind}:${img.file_path}`"
              :title="`${img.width}×${img.height}${img.language ? ` · ${img.language}` : ''}，点击设为${pick.label}`"
              @click="setImage(pick.kind, img.file_path)"
            >
              <img :src="img.url" :alt="pick.label" loading="lazy" />
              <span>{{ img.width }}×{{ img.height }}<span v-if="img.language"> · {{ img.language }}</span></span>
            </button>
            <p v-if="!tmdbImages(pick.kind).length" class="wb-images__empty">TMDB 无候选图</p>
          </div>
        </div>
      </section>

      <section v-if="castSelection.length || data.local.actors.length" class="wb-section">
        <div class="wb-section__head">
          <h3>演员</h3>
          <AppButton
            type="button"
            variant="primary"
            :disabled="savingCast || !castSelection.length"
            @click="saveCast"
          >
            {{ savingCast ? "保存中…" : "保存演员" }}
          </AppButton>
        </div>
        <div v-if="castSelection.length" class="wb-cast">
          <label v-for="actor in castSelection" :key="actor.name" class="wb-cast__row">
            <input v-model="actor.selected" type="checkbox" />
            <img v-if="actor.thumb_url" :src="actor.thumb_url" :alt="actor.name" loading="lazy" />
            <span v-else class="wb-cast__avatar-fallback">{{ actor.name.slice(0, 1) }}</span>
            <span class="wb-cast__info"
              ><b>{{ actor.name }}</b
              ><small>{{ actor.role }}</small></span
            >
          </label>
        </div>
        <p v-else class="wb-hint">TMDB 无演员数据，本地已有 {{ data.local.actors.length }} 位演员。</p>
      </section>

      <section v-if="data.media_type === 'tv'" class="wb-section">
        <div class="wb-section__head">
          <h3>剧集</h3>
          <AppButton
            type="button"
            variant="secondary"
            :disabled="savingSeries || !data.tmdb_available"
            @click="saveSeries"
          >
            {{ savingSeries ? "保存中…" : "整剧按 TMDB 保存" }}
          </AppButton>
        </div>
        <p class="wb-hint">整剧保存会按 TMDB 覆盖剧集 NFO、演员、季海报与分集 NFO / 剧照。</p>
        <div v-for="season in data.seasons || []" :key="season.number" class="wb-season">
          <h4>
            第 {{ season.number }} 季<span v-if="season.name"> · {{ season.name }}</span>
            <span v-if="season.local_poster" class="wb-season__has">本地已有季海报</span>
          </h4>
          <div class="wb-images__grid">
            <button
              v-for="img in season.posters || []"
              :key="img.file_path"
              type="button"
              class="wb-images__card"
              :disabled="settingImageKey === `season_poster:${img.file_path}`"
              :title="`${img.width}×${img.height}，点击设为第 ${season.number} 季海报`"
              @click="setImage('season_poster', img.file_path, season.number)"
            >
              <img :src="img.url" :alt="`第 ${season.number} 季海报`" loading="lazy" />
              <span>{{ img.width }}×{{ img.height }}</span>
            </button>
            <p v-if="!(season.posters || []).length" class="wb-images__empty">TMDB 无本季海报</p>
          </div>
          <div class="wb-episodes">
            <div v-for="ep in season.episodes || []" :key="ep.episode" class="wb-episode">
              <button
                type="button"
                class="wb-episode__head"
                @click="toggleEpisode(season.number, ep.episode)"
              >
                <span>S{{ season.number }}E{{ ep.episode }} · {{ ep.title }}</span>
                <span class="wb-episode__state"
                  >{{ ep.local_nfo ? "已有 NFO" : "缺 NFO" }} ·
                  {{ ep.local_thumb ? "已有剧照" : "缺剧照" }}</span
                >
              </button>
              <div v-if="openEpisodeKey === episodeKey(season.number, ep.episode)" class="wb-episode__body">
                <label class="wb-ep-field"
                  ><span>标题</span
                  ><input :value="episodeEdit(ep).title" type="text" @input="episodeEdit(ep).title = ($event.target as HTMLInputElement).value"
                /></label>
                <label class="wb-ep-field"
                  ><span>简介</span
                  ><textarea
                    :value="episodeEdit(ep).plot"
                    rows="2"
                    @input="episodeEdit(ep).plot = ($event.target as HTMLTextAreaElement).value"
                  />
                </label>
                <label class="wb-ep-field"
                  ><span>播出日</span
                  ><input :value="episodeEdit(ep).aired" type="text" @input="episodeEdit(ep).aired = ($event.target as HTMLInputElement).value"
                /></label>
                <label class="wb-ep-field"
                  ><span>片长（分钟）</span
                  ><input
                    :value="episodeEdit(ep).runtime || ''"
                    type="number"
                    @input="episodeEdit(ep).runtime = Number(($event.target as HTMLInputElement).value) || 0"
                /></label>
                <div class="wb-ep-still">
                  <span>剧照：</span>
                  <img
                    v-if="ep.still_url"
                    :src="ep.still_url"
                    alt="TMDB 剧照"
                    title="点击设为本集剧照"
                    @click="setImage('episode_thumb', ep.still_path || '', season.number, ep.episode)"
                  />
                  <span v-else>TMDB 无剧照</span>
                  <span v-if="ep.local_thumb">（本地已有剧照）</span>
                </div>
                <AppButton type="button" variant="primary" @click="saveEpisode(ep)">保存本集</AppButton>
              </div>
            </div>
            <p v-if="!(season.episodes || []).length" class="wb-images__empty">本地无本季分集文件</p>
          </div>
        </div>
      </section>
    </div>
  </AppModal>
</template>

<style scoped>
.wb-loading,
.wb-error {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 48px 16px;
  color: var(--text-muted);
  font-size: 14px;
}
.wb {
  display: flex;
  flex-direction: column;
  gap: 22px;
}
.wb-hint {
  margin: 0;
  padding: 10px 12px;
  border: var(--mono-bw, 1px) solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface-sunken);
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.6;
}
.wb-section {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.wb-section h3 {
  margin: 0;
  font-size: 15px;
  font-weight: 700;
  color: var(--text);
}
.wb-section h4 {
  margin: 0;
  font-size: 13px;
  font-weight: 700;
  color: var(--text);
}
.wb-section__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}
.wb-section__foot {
  display: flex;
  justify-content: flex-end;
}
.wb-fields {
  display: flex;
  flex-direction: column;
  border: var(--mono-bw, 1px) solid var(--border);
  border-radius: var(--radius-sm);
  overflow: hidden;
}
.wb-field {
  display: grid;
  grid-template-columns: 84px minmax(0, 1fr) minmax(0, 1fr) minmax(0, 1.25fr) 52px;
  gap: 8px;
  align-items: center;
  padding: 7px 10px;
  border-top: var(--mono-bw, 1px) solid var(--border-soft);
  font-size: 12px;
}
.wb-field:first-child {
  border-top: none;
}
.wb-field--head {
  background: var(--surface-sunken);
  font-weight: 700;
  color: var(--text-muted);
}
.wb-field__label {
  font-weight: 700;
  color: var(--text);
}
.wb-field__val {
  color: var(--text-regular);
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  line-height: 1.5;
  word-break: break-word;
}
.wb-field__val--tmdb {
  color: var(--text-muted);
}
.wb-field__edit input,
.wb-field__edit textarea,
.wb-ep-field input,
.wb-ep-field textarea {
  width: 100%;
  box-sizing: border-box;
  border: var(--mono-bw, 1px) solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--text);
  font-size: 12px;
  line-height: 1.5;
  padding: 6px 8px;
  outline: none;
  resize: vertical;
}
.wb-field__edit input:focus,
.wb-field__edit textarea:focus,
.wb-ep-field input:focus,
.wb-ep-field textarea:focus {
  border-color: var(--mono-accent, var(--brand));
}
.wb-field__use {
  border: var(--mono-bw, 1px) solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--text-regular);
  font-size: 12px;
  padding: 5px 0;
  cursor: pointer;
}
.wb-field__use:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
.wb-nfo {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-word;
  background: var(--surface-sunken);
  border: var(--mono-bw, 1px) solid var(--border);
  border-radius: var(--radius-sm);
  padding: 12px;
  font: 11px/1.55 monospace;
  color: var(--text-regular);
  max-height: 220px;
  overflow: auto;
}
.wb-images {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.wb-images__local {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: var(--text-muted);
}
.wb-images__local img {
  width: 56px;
  height: 84px;
  object-fit: cover;
  border: var(--mono-bw, 1px) solid var(--border);
  border-radius: var(--radius-sm);
}
.wb-images__grid {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.wb-images__card {
  display: flex;
  flex-direction: column;
  gap: 4px;
  width: 92px;
  padding: 0;
  border: var(--mono-bw, 1px) solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  cursor: pointer;
  overflow: hidden;
}
.wb-images__card img {
  width: 100%;
  height: 128px;
  object-fit: cover;
  display: block;
  background: var(--surface-sunken);
}
.wb-images__card span {
  font-size: 11px;
  color: var(--text-muted);
  padding: 2px 4px 5px;
  text-align: center;
}
.wb-images__card:disabled {
  opacity: 0.55;
  cursor: wait;
}
.wb-images__empty {
  margin: 0;
  font-size: 12px;
  color: var(--text-muted);
}
.wb-cast {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 8px;
}
.wb-cast__row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border: var(--mono-bw, 1px) solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  cursor: pointer;
}
.wb-cast__row input[type="checkbox"] {
  flex: none;
  accent-color: var(--mono-accent, var(--brand));
}
.wb-cast__row img,
.wb-cast__avatar-fallback {
  flex: none;
  width: 40px;
  height: 40px;
  border-radius: 50%;
  object-fit: cover;
  border: var(--mono-bw, 1px) solid var(--border);
  background: var(--surface-sunken);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  font-weight: 700;
  color: var(--text-muted);
}
.wb-cast__info {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}
.wb-cast__info b {
  font-size: 13px;
  color: var(--text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.wb-cast__info small {
  font-size: 11px;
  color: var(--text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.wb-season {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 12px;
  border: var(--mono-bw, 1px) solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
}
.wb-season__has {
  font-size: 11px;
  font-weight: 400;
  color: var(--text-muted);
  margin-left: 6px;
}
.wb-episodes {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.wb-episode {
  border: var(--mono-bw, 1px) solid var(--border-soft);
  border-radius: var(--radius-sm);
  overflow: hidden;
}
.wb-episode__head {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 8px 10px;
  border: none;
  background: var(--surface-sunken);
  color: var(--text);
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
  text-align: left;
}
.wb-episode__state {
  font-weight: 400;
  color: var(--text-muted);
  white-space: nowrap;
}
.wb-episode__body {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px;
  align-items: flex-start;
}
.wb-ep-field {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  width: 100%;
  font-size: 12px;
  color: var(--text-muted);
}
.wb-ep-field > span {
  flex: none;
  width: 76px;
  padding-top: 7px;
  font-weight: 700;
}
.wb-ep-still {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: var(--text-muted);
}
.wb-ep-still img {
  width: 112px;
  height: 63px;
  object-fit: cover;
  border: var(--mono-bw, 1px) solid var(--border);
  border-radius: var(--radius-sm);
  cursor: pointer;
}
</style>
