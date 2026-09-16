import test from 'node:test';
import { readFileSync } from 'node:fs';
import { document, setSystemDark } from './helper/browser.js';
import { equal, not, ok, same } from './helper/check.js';
import { staticPath } from './helper/lib.js';
import { THEMES, THEME_AUTO, THEME_DARK, THEME_LIGHT, applyTheme, getTheme, resolvedTheme, setTheme, watchSystemTheme } from '../static/js/theme.js';
import { THEME_KEY } from '../static/js/store.js';

function currentAttribute() {
  return document.documentElement.getAttribute('data-bs-theme');
}

test('主题：三个选项，跟随系统排在最前', () => {
  same('选项与顺序', THEMES.map((theme) => theme.value), [THEME_AUTO, THEME_LIGHT, THEME_DARK]);
});

test('主题：没选过就跟随系统，系统深色即深色', () => {
  setTheme(THEME_AUTO);
  setSystemDark(false);
  equal('系统浅色', resolvedTheme(), THEME_LIGHT);
  setSystemDark(true);
  equal('系统深色', resolvedTheme(), THEME_DARK);
  setSystemDark(false);
});

//固定明暗之后，系统再怎么变都不该动页面——这正是「跟随系统」与「固定」的区别
test('主题：固定明暗时不再看系统偏好', () => {
  setTheme(THEME_LIGHT);
  setSystemDark(true);
  equal('固定浅色', resolvedTheme(), THEME_LIGHT);
  setTheme(THEME_DARK);
  setSystemDark(false);
  equal('固定深色', resolvedTheme(), THEME_DARK);
  setTheme(THEME_AUTO);
  setSystemDark(false);
});

test('主题偏好：落 localStorage，非法值收敛到跟随系统', () => {
  setTheme(THEME_DARK);
  equal('落盘', localStorage.getItem(THEME_KEY), THEME_DARK);
  equal('读回来', getTheme(), THEME_DARK);
  setTheme('霓虹');
  equal('不认识的主题回落跟随系统', getTheme(), THEME_AUTO);
});

//bootstrap 认的就是 html 上的这个属性，落错地方整套配色都不会跟着走
test('主题：落到 html 的 data-bs-theme 上', () => {
  setTheme(THEME_DARK);
  equal('深色', applyTheme(), THEME_DARK);
  equal('属性也写上了', currentAttribute(), THEME_DARK);
  setTheme(THEME_LIGHT);
  applyTheme();
  equal('浅色', currentAttribute(), THEME_LIGHT);
  setTheme(THEME_AUTO);
  setSystemDark(true);
  applyTheme();
  equal('跟随系统时落的是解析出来的那一档', currentAttribute(), THEME_DARK);
  setSystemDark(false);
});

test('主题：跟随系统时系统切换会回调，固定明暗时不回调', () => {
  const seen = [];
  watchSystemTheme((resolved) => seen.push(resolved));
  setTheme(THEME_AUTO);
  setSystemDark(true);
  same('跟随系统时跟着切', seen, [THEME_DARK]);
  setSystemDark(false);
  same('切回来也跟', seen, [THEME_DARK, THEME_LIGHT]);

  setTheme(THEME_DARK);
  setSystemDark(true);
  equal('固定之后系统再变也不回调', seen.length, 2);
  setTheme(THEME_AUTO);
  setSystemDark(false);
});

//图表的默认色是建实例时读一次的，主题换了不重读，深色底上就还印着深色字
test('图表：换主题之后重读正文色', async () => {
  const { refreshChartTheme } = await import('../static/js/chart.js');
  let bodyColor = '#212529';
  globalThis.getComputedStyle = () => ({ getPropertyValue: () => bodyColor, fontFamily: 'system-ui' });
  refreshChartTheme();
  equal('浅色态的正文色', Chart.defaults.color, '#212529');
  bodyColor = '#dee2e6';
  refreshChartTheme();
  equal('深色态的正文色', Chart.defaults.color, '#dee2e6');
  globalThis.getComputedStyle = () => ({ getPropertyValue: () => '', fontFamily: '' });
  refreshChartTheme();
  ok('读不到就回落到一个能看见的颜色', Chart.defaults.color);
});

//首屏那段脚本必须同步跑在样式表之前，换成模块脚本就会被 defer 掉，深色偏好的人先被闪一屏白
test('首屏主题脚本：同步内联，且排在样式表之前', () => {
  const html = readFileSync(staticPath('index.html'), 'utf8');
  const script = html.indexOf('data-bs-theme');
  const inline = html.indexOf("localStorage.getItem('jotcash.theme')");
  const stylesheet = html.indexOf('bootstrap.min.css');
  ok('html 上有初值', script >= 0);
  ok('有内联脚本', inline >= 0);
  ok('脚本在样式表之前', inline < stylesheet);
  not('这段不能是模块脚本', /<script type="module">[\s\S]*jotcash\.theme/.test(html));
});
