package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"stockroom/internal/ledger"
	"strings"
	"testing"
)

func TestStatementHTTPPreviewCommitReplayAndOriginal(t *testing.T) {
	h := testHandler(t)
	account := performJSON[ledger.Account](t, h, "POST", "/api/v1/accounts", `{"name":"native-http","type":"WALLET","initial_balance":100}`, 201)
	file := []byte("交易时间,交易类型,交易对方,商品,收/支,金额(元),支付方式,当前状态,交易单号,商户单号,备注\r\n2026-11-01 12:03:00,商户消费,龙记,,支出,20.00,零钱,支付成功,native-http,,\r\n")
	input := ledger.StatementInput{AccountID: account.ID, Filename: "微信原生.csv", File: file, SyncID: "http-week"}
	b, _ := json.Marshal(input)
	preview := performJSON[ledger.StatementResult](t, h, "POST", "/api/v1/statements/wechat/preview", string(b), 200)
	if preview.Batch.NewTransactions != 1 || preview.PreviewToken == "" || preview.Rows[0].TransactionID != "" || preview.Batch.ID != "" {
		t.Fatal(preview)
	}
	input.PreviewToken = preview.PreviewToken
	b, _ = json.Marshal(input)
	first := keyedRequest(t, h, "POST", "/api/v1/statements/wechat/commit", string(b), "statement-http", 201)
	var result ledger.StatementResult
	if e := json.Unmarshal(first.Body.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	repeated := keyedRequest(t, h, "POST", "/api/v1/statements/wechat/commit", string(b), "statement-http", 201)
	if repeated.Body.String() != first.Body.String() {
		t.Fatal("replay changed response")
	}
	raw := httptest.NewRecorder()
	h.ServeHTTP(raw, httptest.NewRequest("GET", "/api/v1/statements/batches/"+result.Batch.ID+"/file", nil))
	if raw.Code != 200 || raw.Body.String() != string(file) || !strings.Contains(raw.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal(raw.Code, raw.Body.String())
	}
	summary := performJSON[ledger.StatementSummary](t, h, "GET", "/api/v1/statements/summary?sync_id=http-week", "", 200)
	if summary.Totals.NewTransactions != 1 || summary.Totals.Unannotated != 1 {
		t.Fatal(summary)
	}
	performJSON[map[string]any](t, h, "GET", "/api/v1/statements/batches?limit=20&offset=0", "", 200)
	performJSON[map[string]any](t, h, "POST", "/api/v1/statements/alipay/preview", string(b), 422)
	performJSON[map[string]any](t, h, "GET", "/api/v1/statements/batches?limit=bad", "", 422)
	input.PreviewToken = ""
	b, _ = json.Marshal(input)
	keyedRequest(t, h, "POST", "/api/v1/statements/wechat/commit", string(b), "no-preview", 422)
	// Served contract must include native endpoints and closed request/response schemas.
	var doc map[string]any
	contract, e := os.ReadFile(filepath.Join("..", "web", "static", "openapi.json"))
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(contract, &doc); e != nil {
		t.Fatal(e)
	}
	if _, ok := doc["paths"].(map[string]any)["/api/v1/statements/{provider}/preview"]; !ok {
		t.Fatal("native import contract missing")
	}
}
