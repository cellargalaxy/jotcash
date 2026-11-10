// Package parsers 是生效识别器的唯一清单：浏览器端的 wasm 入口引用它，契约测试也拿它对账。
// 服务端不引用它，识别器的代码与依赖（PDF 解析等）都不进服务端二进制
package parsers

import (
	_ "github.com/cellargalaxy/jotcash/service/expense/base_csv"
	_ "github.com/cellargalaxy/jotcash/service/expense/cmb_debit"
	_ "github.com/cellargalaxy/jotcash/service/expense/icbc_credit"
	_ "github.com/cellargalaxy/jotcash/service/expense/unionpay_screenshot"
)
