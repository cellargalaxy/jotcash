package handler_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/cellargalaxy/go_common/util"
	"github.com/cellargalaxy/jotcash/config"
	"github.com/cellargalaxy/jotcash/db"
	"github.com/cellargalaxy/jotcash/model"
	"github.com/cellargalaxy/jotcash/tool"
	"github.com/gin-gonic/gin"
)

func switchCurrency(t *testing.T, engine *gin.Engine, jwt string) expenseResp {
	t.Helper()
	var resp expenseResp
	doRequest(t, engine, newRequest(model.PathCurrencySwitch, jwt, nil), &resp)
	return resp
}

// F-4：把记账币种不是当前选择的明细逐笔重新折算，含已删除的行
func TestSwitchCurrency(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig().ServerToken, clientToken, time.Hour)
	ctx := newTokenCtx(clientToken)

	stay := newTestExpense("已是目标币种的店", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), "100")
	switched := newTestExpense("待切换的店", time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC), "200")
	switched.AccountingCurrency = "JPY"
	gone := newTestExpense("已删除的店", time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC), "300")
	gone.AccountingCurrency = "JPY"
	if _, err := db.InsertExpense(ctx, stay, switched, gone); err != nil {
		t.Fatalf("插入测试明细异常: %+v", err)
	}
	if _, err := db.DeleteExpense(ctx, model.ExpenseInquiry{Id: []int64{gone.Id}}); err != nil {
		t.Fatalf("软删除测试明细异常: %+v", err)
	}

	resp := switchCurrency(t, engine, jwt)
	if resp.Code != http.StatusOK {
		t.Fatalf("记账币种切换应成功: %+v", resp)
	}
	if resp.Data.Count != 2 {
		t.Fatalf("应切换2笔: got=%d want=2", resp.Data.Count)
	}

	objects := selectExpense(t, engine, jwt, model.ExpenseInquiry{Deleted: model.DeletedAll}).Data.Object
	for i := range objects {
		if objects[i].AccountingCurrency != testAccountingCurrency {
			t.Fatalf("记账币种应全部切到目标: %+v", objects[i])
		}
		if objects[i].Id == stay.Id {
			//已经是目标币种的明细一笔都不该被动，连版本号都不该涨
			if objects[i].Version != stay.Version {
				t.Errorf("已是目标币种的明细不该被动: %+v", objects[i])
			}
			continue
		}
		//F-4：汇率无条件按支出日期与目标币种重取，记账金额跟着重算
		if !objects[i].ExchangeRate.IsPositive() || !objects[i].AccountingAmount.Equal(objects[i].ExpenseAmount.Mul(objects[i].ExchangeRate).Round(2)) {
			t.Errorf("记账金额应重算: %+v", objects[i])
		}
		if objects[i].Id == gone.Id && !objects[i].DeletedAt.Valid {
			t.Errorf("切换不该把已删除的行改活: %+v", objects[i])
		}
	}

	logResp := selectOperationLog(t, engine, jwt, model.OperationLogInquiry{OperationType: []string{model.OperationTypeCurrencySwitch}})
	if logResp.Data.Count != 1 || logResp.Data.Object[0].Result != model.ResultSuccess {
		t.Fatalf("应记一条成功的切换审计: %+v", logResp.Data)
	}

	//切完再切一次是空转，仍旧记一条审计
	if resp = switchCurrency(t, engine, jwt); resp.Data.Count != 0 {
		t.Errorf("重试应无可切换的明细: %+v", resp.Data)
	}
}

// F-4：允许部分失败，切到一半卡住时已切的那些要留下，审计记「部分成功」
func TestSwitchCurrencyPartial(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig().ServerToken, clientToken, time.Hour)
	ctx := newTokenCtx(clientToken)

	//支出币种不在枚举内的明细切不动，日期排在后面，好让前一笔先切成功
	good := newTestExpense("能切的店", time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC), "100")
	good.AccountingCurrency = "JPY"
	bad := newTestExpense("切不动的店", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), "200")
	bad.AccountingCurrency = "JPY"
	bad.ExpenseCurrency = "XYZ"
	if _, err := db.InsertExpense(ctx, good, bad); err != nil {
		t.Fatalf("插入测试明细异常: %+v", err)
	}

	if resp := switchCurrency(t, engine, jwt); resp.Code == http.StatusOK {
		t.Errorf("切不动的明细应报错: %+v", resp)
	}
	logResp := selectOperationLog(t, engine, jwt, model.OperationLogInquiry{OperationType: []string{model.OperationTypeCurrencySwitch}})
	if logResp.Data.Count != 1 || logResp.Data.Object[0].Result != model.ResultPartial {
		t.Fatalf("应记一条部分成功的切换审计: %+v", logResp.Data)
	}

	objects := selectExpense(t, engine, jwt, model.ExpenseInquiry{Id: []int64{good.Id}}).Data.Object
	if len(objects) != 1 || objects[0].AccountingCurrency != testAccountingCurrency {
		t.Errorf("切成功的那笔应留下: %+v", objects)
	}
}

// 库里一笔都切不动时是「失败」，不是「部分成功」
func TestSwitchCurrencyFailure(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt := newJwt(t, config.GetConfig().ServerToken, clientToken, time.Hour)
	ctx := newTokenCtx(clientToken)

	bad := newTestExpense("切不动的店", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC), "100")
	bad.AccountingCurrency = "JPY"
	bad.ExpenseCurrency = "XYZ"
	if _, err := db.InsertExpense(ctx, bad); err != nil {
		t.Fatalf("插入测试明细异常: %+v", err)
	}

	if resp := switchCurrency(t, engine, jwt); resp.Code == http.StatusOK {
		t.Errorf("切不动的明细应报错: %+v", resp)
	}
	logResp := selectOperationLog(t, engine, jwt, model.OperationLogInquiry{OperationType: []string{model.OperationTypeCurrencySwitch}})
	if logResp.Data.Count != 1 || logResp.Data.Object[0].Result != model.ResultFailure {
		t.Errorf("应记一条失败的切换审计: %+v", logResp.Data)
	}
}

func TestSwitchCurrencyWithoutJwt(t *testing.T) {
	engine, _ := newTestEngine(t)

	if resp := switchCurrency(t, engine, ""); resp.Code != http.StatusUnauthorized {
		t.Errorf("没带jwt应401: %+v", resp)
	}
}

// 记账币种逐请求携带，jwt里没带就不知道要切到哪去
func TestSwitchCurrencyWithoutCurrency(t *testing.T) {
	engine, clientToken := newTestEngine(t)
	jwt, err := tool.EnJwt(util.GenCtx(), config.GetConfig().ServerToken, clientToken, "", time.Hour)
	if err != nil {
		t.Fatalf("签发jwt异常: %+v", err)
	}

	if resp := switchCurrency(t, engine, jwt); resp.Code == http.StatusOK {
		t.Errorf("没带记账币种应报错: %+v", resp)
	}
}
