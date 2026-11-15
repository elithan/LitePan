import { ref } from "vue";

export type ThemePref = "light" | "dark" | "auto";
export type SkinPref = "default" | "brutal" | "mono";

const KEY = "litepan_theme";
const KEY_SKIN = "litepan_skin";
const THEME_ORDER: ThemePref[] = ["auto", "light", "dark"];

const THEME_LABELS: Record<ThemePref, string> = {
  auto: "跟随系统",
  light: "浅色主题",
  dark: "深色主题",
};

let mediaQuery: MediaQueryList | null = null;

function readSkinPref(): SkinPref {
  const v = localStorage.getItem(KEY_SKIN);
  if (v === "brutal") return "brutal";
  if (v === "mono") return "mono";
  // 黑白简约是新的默认风格；旧的“经典主题”保存值也迁移到 mono。
  return "mono";
}

const skinState = ref<SkinPref>(readSkinPref());

export function supportsThemeToggle(): boolean {
  return skinState.value !== "brutal";
}

function resolve(pref: ThemePref): "light" | "dark" {
  if (pref === "auto") {
    return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  }
  return pref;
}

function activeSkin(): SkinPref {
  if (typeof document === "undefined") {
    return skinState.value;
  }
  const skin = document.documentElement.dataset.skin;
  if (skin === "brutal") return "brutal";
  if (skin === "mono") return "mono";
  return "default";
}

function applyThemeDataset(pref: ThemePref): void {
  if (activeSkin() === "brutal") {
    document.documentElement.dataset.theme = "light";
    return;
  }
  document.documentElement.dataset.theme = resolve(pref);
}

function bindSystemThemeListener(): void {
  if (typeof window === "undefined" || !window.matchMedia || mediaQuery) return;
  mediaQuery = window.matchMedia("(prefers-color-scheme: dark)");
  mediaQuery.addEventListener("change", () => {
    if (getThemePref() === "auto" && activeSkin() !== "brutal") {
      applyThemeDataset("auto");
    }
  });
}

export function isValidThemePref(v: string): v is ThemePref {
  return v === "light" || v === "dark" || v === "auto";
}

export function getThemeLabel(pref: ThemePref): string {
  return THEME_LABELS[pref];
}

export function getThemeToggleTitle(pref: ThemePref): string {
  return `当前：${getThemeLabel(pref)}，点击切换主题`;
}

export function getNextThemePref(pref: ThemePref): ThemePref {
  const index = THEME_ORDER.indexOf(pref);
  return THEME_ORDER[(index + 1) % THEME_ORDER.length] ?? "light";
}

export function getThemePref(): ThemePref {
  const v = localStorage.getItem(KEY) ?? "";
  // 默认跟随系统深浅色，与黑白简约皮肤配合。
  return isValidThemePref(v) ? v : "auto";
}

export function setThemePref(pref: ThemePref): void {
  localStorage.setItem(KEY, pref);
  applyThemeDataset(pref);
}

export function applySkin(skin: SkinPref): void {
  document.documentElement.dataset.skin = skin;
  applyThemeDataset(getThemePref());
}

export function getSkinPref(): SkinPref {
  return skinState.value;
}

export function setSkinPref(skin: SkinPref): void {
  localStorage.setItem(KEY_SKIN, skin);
  skinState.value = skin;
  applySkin(skin);
}

export function previewSkin(skin: SkinPref): void {
  applySkin(skin);
}

export function restoreSavedSkin(): void {
  applySkin(skinState.value);
}

export function initTheme(): void {
  skinState.value = readSkinPref();
  applySkin(skinState.value);
  bindSystemThemeListener();
}
