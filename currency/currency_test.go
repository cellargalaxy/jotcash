package currency

import (
	"os"
	"testing"

	"github.com/cellargalaxy/go_common/util"
)

func TestMain(m *testing.M) {
	code := m.Run()
	os.RemoveAll("log")
	os.Exit(code)
}

func TestCheckCode(t *testing.T) {
	ctx := util.GenCtx()

	for _, code := range []string{"CNY", "USD", "JPY", "KWD"} {
		if err := CheckCode(ctx, code); err != nil {
			t.Errorf("合法币种应通过: code=%s err=%+v", code, err)
		}
	}
	for _, code := range []string{"", "ABC", "XXX", "cny"} {
		if err := CheckCode(ctx, code); err == nil {
			t.Errorf("非法币种应报错: code=%q", code)
		}
	}
}

func TestGetDigits(t *testing.T) {
	ctx := util.GenCtx()

	//小数位数不是都等于2，记账金额的舍入得按币种来
	for code, want := range map[string]int{"CNY": 2, "USD": 2, "JPY": 0, "KWD": 3} {
		digits, err := GetDigits(ctx, code)
		if err != nil {
			t.Fatalf("取小数位数异常: code=%s err=%+v", code, err)
		}
		if digits != want {
			t.Errorf("小数位数不符: code=%s got=%d want=%d", code, digits, want)
		}
	}
	if _, err := GetDigits(ctx, "ABC"); err == nil {
		t.Errorf("非法币种应报错")
	}
}

func TestGetCurrency(t *testing.T) {
	ctx := util.GenCtx()

	object, err := GetCurrency(ctx, "CNY")
	if err != nil {
		t.Fatalf("取币种异常: %+v", err)
	}
	if object.Code != "CNY" || object.Digits != 2 || object.Symbol == "" || !object.Enabled {
		t.Errorf("币种字段不符: %s", object)
	}
	if _, err = GetCurrency(ctx, "ABC"); err == nil {
		t.Errorf("非法币种应报错")
	}
}

func TestListCurrency(t *testing.T) {
	ctx := util.GenCtx()

	objects := ListCurrency(ctx)
	if len(objects) == 0 {
		t.Fatalf("币种枚举为空")
	}
	var found bool
	for i := range objects {
		if objects[i].Code == "" {
			t.Fatalf("币种代码为空: %s", objects[i])
		}
		if objects[i].Code == "CNY" {
			found = true
		}
	}
	if !found {
		t.Errorf("币种枚举里没有CNY")
	}
}

func TestDisabledCode(t *testing.T) {
	ctx := util.GenCtx()

	disabledCodes["USD"] = true
	t.Cleanup(func() { delete(disabledCodes, "USD") })

	if err := CheckCode(ctx, "USD"); err == nil {
		t.Errorf("停用的币种不该通过校验")
	}
	//停用只是不给录入，库里已有明细的小数位数还得算得出来
	if digits, err := GetDigits(ctx, "USD"); err != nil || digits != 2 {
		t.Errorf("停用的币种仍应有小数位数: digits=%d err=%+v", digits, err)
	}
	if _, err := GetCurrency(ctx, "USD"); err == nil {
		t.Errorf("停用的币种不该取到")
	}
	for _, object := range ListCurrency(ctx) {
		if object.Code == "USD" && object.Enabled {
			t.Errorf("停用的币种在枚举里应标为未启用: %s", object)
		}
	}
}
