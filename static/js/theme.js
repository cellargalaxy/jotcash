import { getTheme as readTheme, setTheme as writeTheme } from './store.js';

export const THEME_AUTO = 'auto';
export const THEME_LIGHT = 'light';
export const THEME_DARK = 'dark';

export const THEMES = [
  { value: THEME_AUTO, name: '跟随系统' },
  { value: THEME_LIGHT, name: '浅色' },
  { value: THEME_DARK, name: '深色' },
];

const DARK_QUERY = '(prefers-color-scheme: dark)';

let theme = '';

function mediaQuery() {
  return typeof matchMedia === 'function' ? matchMedia(DARK_QUERY) : null;
}

export function getTheme() {
  if (!theme) {
    const saved = readTheme();
    theme = THEMES.some((item) => item.value === saved) ? saved : THEME_AUTO;
  }
  return theme;
}

export function setTheme(value) {
  theme = THEMES.some((item) => item.value === value) ? value : THEME_AUTO;
  writeTheme(theme);
}

//落到 html 上的只有明暗两种，「跟随系统」是选项不是取值
export function resolvedTheme() {
  const current = getTheme();
  if (current !== THEME_AUTO) return current;
  const query = mediaQuery();
  return query && query.matches ? THEME_DARK : THEME_LIGHT;
}

//bootstrap 5.3 认的就是 html 上的 data-bs-theme，自身与所有组件的配色跟着它走，不必另写一套暗色 CSS
export function applyTheme() {
  const resolved = resolvedTheme();
  document.documentElement.setAttribute('data-bs-theme', resolved);
  return resolved;
}

//「跟随系统」下系统半夜自己切了，页面也得跟上；固定明暗时这一枪不该改动页面
export function watchSystemTheme(onChange) {
  const query = mediaQuery();
  if (!query || typeof query.addEventListener !== 'function') return;
  query.addEventListener('change', () => {
    if (getTheme() === THEME_AUTO) onChange(applyTheme());
  });
}
