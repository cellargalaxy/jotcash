import test from 'node:test';
import './helper/browser.js';
import { equal, includes, not, ok, same } from './helper/check.js';
import {
  addMonth,
  compact,
  currencyDigits,
  currencyName,
  dateToRfc3339,
  formatAmount,
  formatDate,
  formatDateTime,
  formatFileSize,
  formatMonth,
  isDecimal,
  isPositiveDecimal,
  monthOf,
  multiplyAmount,
  parseCsv,
  toCsv,
} from '../static/js/util.js';

test('金额格式化：后端给的精确十进制串原样保留，只补不截', () => {
  equal('整数补到金额精度', formatAmount('68'), '68.00');
  equal('两位照旧', formatAmount('68.00'), '68.00');
  equal('三位不截', formatAmount('12.345'), '12.345');
  equal('长串不动', formatAmount('100.00000000000001'), '100.00000000000001');
  equal('大数不动', formatAmount('123456789012.34'), '123456789012.34');
  equal('负数', formatAmount('-120'), '-120.00');
  equal('空值', formatAmount(''), '');
  equal('null', formatAmount(null), '');
  equal('非法值原样返回', formatAmount('abc'), 'abc');
});

//number 的小数位数一量就是十几位，「只补不截」会把浮点噪声整条印到屏幕上
test('金额格式化：number 先收 15 位有效数字再量小数位', () => {
  equal('0.01+0.14+0.15', formatAmount(0.01 + 0.14 + 0.15), '0.30');
  equal('0.02+0.15+0.17', formatAmount(0.02 + 0.15 + 0.17), '0.34');
  equal('0.1+0.2', formatAmount(0.1 + 0.2), '0.30');
  equal('0.7+0.1', formatAmount(0.7 + 0.1), '0.80');
  equal('4.35*100', formatAmount(4.35 * 100), '435.00');
  equal('负数取反', formatAmount(-(0.1 + 0.2)), '-0.30');
  //噪声在第 16 位往后，第 3 位小数的 5 是真值，不能跟着被截掉
  equal('1.005*3', formatAmount(1.005 * 3), '3.015');
  equal('真实四位小数保留', formatAmount(12.3456), '12.3456');
  equal('零', formatAmount(0), '0.00');
});

test('时间展示：零值与空值都收敛成空串', () => {
  equal('日期', formatDate('2026-09-15T10:12:33+08:00'), '2026-09-15');
  equal('月份', formatMonth('2026-09-15T10:12:33+08:00'), '2026-09');
  includes('日期时间', formatDateTime('2026-09-15T10:12:33+08:00'), '2026-09-15 ');
  equal('Go 的零值时间', formatDate('0001-01-01T00:00:00Z'), '');
  equal('空串', formatDate(''), '');
  equal('null', formatDate(null), '');
  equal('非法串', formatDate('哪天'), '');
});

test('日期转 RFC3339：补时刻与本地时区偏移', () => {
  const start = dateToRfc3339('2026-09-15');
  const end = dateToRfc3339('2026-09-15', true);
  includes('起始时刻', start, '2026-09-15T00:00:00');
  includes('结束时刻', end, '2026-09-15T23:59:59');
  ok('带时区偏移', /[+-]\d{2}:\d{2}$/.test(start));
  equal('空值不拼', dateToRfc3339(''), '');
});

test('月份推移：跨年与跨多年都按自然月走', () => {
  equal('同年', addMonth('2026-09', 2), '2026-11');
  equal('跨年', addMonth('2026-11', 2), '2027-01');
  equal('整年', addMonth('2026-09', 12), '2027-09');
  equal('原地', addMonth('2026-09', 0), '2026-09');
  equal('倒退', addMonth('2026-01', -1), '2025-12');
  equal('空值', addMonth('', 3), '');
  equal('取月份', monthOf('2026-09-15'), '2026-09');
  equal('取月份空值', monthOf(''), '');
});

test('记账金额：与后端 fillExpense 同一个算法，按金额精度取整', () => {
  equal('常规', multiplyAmount('128.5', '1'), '128.5');
  equal('折算', multiplyAmount('100', '7.12'), '712');
  equal('四舍五入到两位', multiplyAmount('99.99', '0.048'), '4.8');
  equal('入位', multiplyAmount('1.005', '1'), '1.01');
  equal('空值当 0', multiplyAmount('', ''), '0');
  equal('非法值', multiplyAmount('abc', '1'), '');
});

test('金额判定：非法输入一律不抛异常', () => {
  ok('正数', isPositiveDecimal('0.01'));
  not('零', isPositiveDecimal('0'));
  not('负数', isPositiveDecimal('-1'));
  not('非法', isPositiveDecimal('abc'));
  ok('负数是合法十进制', isDecimal('-120.00'));
  ok('零', isDecimal('0'));
  not('空串', isDecimal(''));
  not('非法', isDecimal('1.2.3'));
});

test('文件大小：进位到 1024 才换单位', () => {
  equal('字节', formatFileSize(512), '512 B');
  equal('整千字节', formatFileSize(1024), '1.00 KB');
  equal('兆', formatFileSize(1024 * 1024), '1.00 MB');
  equal('吉', formatFileSize(1024 * 1024 * 1024), '1.00 GB');
  equal('零', formatFileSize(0), '0 B');
});

//CSV 的生成与解析是一对镜像，只有往返恒等才算测到位
test('CSV 往返恒等：逗号、引号、换行、中文都能原样回来', () => {
  const header = ['支出日期', '交易备注', '支出金额'];
  const rows = [
    ['2026-09-15', '含逗号,的备注', '128.50'],
    ['2026-09-16', '含"引号"的备注', '-12.00'],
    ['2026-09-17', '含\r\n换行的备注', '0'],
    ['2026-09-18', '', '68.00'],
  ];
  const parsed = parseCsv(toCsv(header, rows));
  same('表头', parsed[0], header);
  same('数据行', parsed.slice(1), rows);
});

test('CSV 解析：BOM、末尾空行与单列行的边界', () => {
  const parsed = parseCsv('﻿甲,乙\r\n1,2\r\n');
  same('BOM 不进第一格', parsed[0], ['甲', '乙']);
  equal('末尾空行不算数据行', parsed.length, 2);
  equal('空文本', parseCsv('').length, 0);
  same('只有一行', parseCsv('甲,乙'), [['甲', '乙']]);
});

test('数组去空：留空的筛选条件不能进请求体', () => {
  same('去空去空白', compact([' a ', '', '  ', 'b']), ['a', 'b']);
  same('数字转字符串', compact([1, 2]), ['1', '2']);
  same('null 入参', compact(null), []);
});

test('币种：表内取精度与名称，表外原样回退', () => {
  equal('人民币两位', currencyDigits('CNY'), 2);
  equal('日元零位', currencyDigits('JPY'), 0);
  equal('表外回退金额精度', currencyDigits('XXX'), 2);
  equal('带中文名', currencyName('CNY'), 'CNY 人民币');
  equal('表外只给代码', currencyName('XXX'), 'XXX');
});
