import { getLang as readLang, setLang as writeLang } from './store.js';
import { SERVER as EN_SERVER, TEXT as EN_TEXT } from './lang_en.js';

export const LANG_ZH = 'zh';
export const LANG_EN = 'en';

//下拉里的语言名一律用该语言自己的写法，不翻译：看不懂当前语言的人，正是靠这个找到自己那一项
export const LANGS = [
  { value: LANG_ZH, name: '中文' },
  { value: LANG_EN, name: 'English' },
];

//词条以中文原文为键，中文即原文，所以没有中文词典。
//另立一份中文词典等于给每句话存两遍，改了代码忘了改词典就是静默漏译，这种副本迟早漂移
const DICTS = {
  [LANG_EN]: { text: EN_TEXT, server: EN_SERVER },
};

let lang = '';

//「浏览器是中文则中文，其余都英文」：只看首选语言。
//拿整条 languages 列表去找中文会让首选英文、备选中文的浏览器落到中文，与这条判据相反
export function detectLang() {
  const nav = typeof navigator === 'undefined' ? null : navigator;
  const preferred = (nav && (nav.language || (nav.languages || [])[0])) || '';
  return String(preferred).toLowerCase().startsWith('zh') ? LANG_ZH : LANG_EN;
}

export function getLang() {
  if (!lang) {
    const saved = readLang();
    lang = saved === LANG_ZH || saved === LANG_EN ? saved : detectLang();
  }
  return lang;
}

export function setLang(value) {
  lang = value === LANG_EN ? LANG_EN : LANG_ZH;
  writeLang(lang);
}

//占位符写成 {名字}：位置参数在两种语序之间必然错位，英文里把主宾调个头就对不上了
function format(template, params) {
  if (!params) return template;
  return template.replace(/\{(\w+)\}/g, (whole, name) => (params[name] === undefined ? whole : String(params[name])));
}

export function t(key, params) {
  const dict = DICTS[getLang()];
  return format((dict && dict.text[key]) || key, params);
}

//后端不改，文案原样是中文，转译只能在前端做：先按整句命中，再按模板匹配。
//模板里的 $1 会再走一次转译，因为后端的报错是嵌套拼出来的（外层报位置，内层报原因）；
//不是文案的那部分（行号、币种代码、文件名）在词表里命中不了，原样返回。
//每条模板的捕获组都严格短于原串，递归必然收敛
export function serverText(text) {
  const raw = text === null || text === undefined ? '' : String(text);
  const dict = DICTS[getLang()];
  if (!dict) return raw;
  if (dict.text[raw]) return dict.text[raw];
  for (const rule of dict.server) {
    const matched = rule.pattern.exec(raw);
    if (!matched) continue;
    return rule.text.replace(/\$(\d)/g, (whole, index) => serverText(matched[Number(index)]));
  }
  return raw;
}
