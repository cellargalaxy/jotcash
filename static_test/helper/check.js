//断言一律手写，不引入断言库。这里的每个函数都只做两件事：
//把实际值与期望值拼进失败信息，再把自己这一帧从栈里砍掉——否则失败会报在本文件里，
//而不是报在真正出问题的用例行上
function fail(assert, message) {
  const err = new Error(message);
  Error.captureStackTrace(err, assert);
  throw err;
}

function show(value) {
  if (value === null || value === undefined) return String(value);
  if (typeof value === 'string') return JSON.stringify(value);
  if (typeof value === 'object') return JSON.stringify(value);
  return String(value);
}

export function equal(name, got, want) {
  if (got !== want) fail(equal, `${name}: got=${show(got)} want=${show(want)}`);
}

export function ok(name, got) {
  if (!got) fail(ok, `${name}: got=${show(got)} want=真值`);
}

export function not(name, got) {
  if (got) fail(not, `${name}: got=${show(got)} want=假值`);
}

//数组与对象按 JSON 比对：这里比的都是纯数据，不必处理循环引用
export function same(name, got, want) {
  const gotText = JSON.stringify(got);
  const wantText = JSON.stringify(want);
  if (gotText !== wantText) fail(same, `${name}: got=${gotText} want=${wantText}`);
}

export function includes(name, text, part) {
  if (!String(text).includes(part)) fail(includes, `${name}: got=${show(text)} want=包含${show(part)}`);
}

export function excludes(name, text, part) {
  if (String(text).includes(part)) fail(excludes, `${name}: got=${show(text)} want=不含${show(part)}`);
}

//浮点比对必须给容差，图表喂的是 number，位数超了就不可能逐位相等
export function near(name, got, want, epsilon) {
  const limit = epsilon === undefined ? 1e-9 : epsilon;
  if (!(Math.abs(got - want) <= limit)) fail(near, `${name}: got=${show(got)} want=${show(want)}±${limit}`);
}

//异常分支同样是被测行为：只断言「报错了」不够，文案也得对上
export async function rejects(name, promise, part) {
  let message = '';
  try {
    await promise;
  } catch (err) {
    message = err && err.message ? err.message : String(err);
  }
  if (message === '') fail(rejects, `${name}: got=未报错 want=报错并包含${show(part)}`);
  if (part !== undefined && !message.includes(part)) {
    fail(rejects, `${name}: got=${show(message)} want=包含${show(part)}`);
  }
  return message;
}
