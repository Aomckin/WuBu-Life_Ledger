// Package statement adapts double-entry-generator providers to lossless Wubu evidence.
// Upstream parsing is pinned in go.mod; no Beancount compiler or classification rules run.
package statement

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/deb-sig/double-entry-generator/v2/pkg/ir"
	"github.com/deb-sig/double-entry-generator/v2/pkg/provider"
	"github.com/deb-sig/double-entry-generator/v2/pkg/provider/alipay"
	"github.com/deb-sig/double-entry-generator/v2/pkg/provider/citic"
	"github.com/deb-sig/double-entry-generator/v2/pkg/provider/spdb_debit"
	"github.com/deb-sig/double-entry-generator/v2/pkg/provider/wechat"
	"github.com/shakinm/xlsReader/xls"
	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/simplifiedchinese"
	"stockroom/internal/ledger"
)

const Version = "double-entry-generator/v2 v2.16.0 + wubu.1"
const MaxFileBytes = 5 << 20
const MaxRows = 2000

type Template struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Group      string   `json:"group"`
	Extensions []string `json:"extensions"`
}

// Includes every bank registered by upstream v2.16.0; securities/crypto providers are not exposed.
var Templates = []Template{
	{"wechat", "微信支付", "payment", []string{".xlsx", ".csv"}}, {"alipay", "支付宝", "payment", []string{".csv"}},
	{"abc_debit", "农业银行借记卡", "bank", []string{".csv"}}, {"bmo", "BMO", "bank", []string{".csv"}},
	{"boc", "中国银行", "bank", []string{".csv"}}, {"bocom_credit", "交通银行信用卡", "bank", []string{".csv"}},
	{"bocom_debit", "交通银行借记卡", "bank", []string{".csv"}}, {"ccb", "建设银行", "bank", []string{".csv", ".xls", ".xlsx"}},
	{"cgb_credit", "广发银行信用卡", "bank", []string{".csv"}}, {"cib_debit", "兴业银行借记卡", "bank", []string{".xls", ".xlsx"}},
	{"citic", "中信银行信用卡", "bank", []string{".xls"}}, {"cmb", "招商银行", "bank", []string{".csv"}},
	{"hsbchk", "汇丰香港", "bank", []string{".csv"}}, {"icbc", "工商银行", "bank", []string{".csv"}},
	{"spdb_debit", "浦发银行借记卡", "bank", []string{".xls"}}, {"td", "TD", "bank", []string{".csv"}},
}

func Lookup(code string) (Template, bool) {
	for _, t := range Templates {
		if t.ID == code {
			return t, true
		}
	}
	return Template{}, false
}

// DetectPayment recognizes wallet exports using the same native headers as Parse.
// Bank headers can overlap, so the caller must choose a bank template.
func DetectPayment(filename string, data []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	if len(data) == 0 || len(data) > MaxFileBytes {
		return "", ledger.ErrInvalid
	}
	for _, t := range Templates[:2] {
		for _, supported := range t.Extensions {
			if ext != supported {
				continue
			}
			rows, err := readRecords(t.ID, ext, data)
			if err == nil {
				if _, _, err = dataStart(t.ID, rows); err == nil {
					return t.ID, nil
				}
			}
		}
	}
	return "", nil
}

type Row struct {
	Number    int       `json:"row"`
	Values    []string  `json:"values"`
	RawRecord string    `json:"raw_record"`
	Header    []string  `json:"header"`
	Order     *ir.Order `json:"order,omitempty"`
	Error     string    `json:"error,omitempty"`
}
type record struct {
	values []string
	raw    string
	number int
}

// Parse keeps every data row, including rows rejected or filtered by upstream.
// Only a broken document or a wrong provider header rejects the whole preview.
func Parse(code, filename string, data []byte) (out []Row, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			out = nil
			err = fmt.Errorf("%w: 无法读取原生账单: %v", ledger.ErrInvalid, failure)
		}
	}()
	t, ok := Lookup(code)
	if !ok {
		return nil, fmt.Errorf("%w: 未支持的账单模板", ledger.ErrInvalid)
	}
	ext := strings.ToLower(filepath.Ext(filename))
	valid := false
	for _, e := range t.Extensions {
		valid = valid || ext == e
	}
	if !valid || len(data) == 0 || len(data) > MaxFileBytes {
		return nil, fmt.Errorf("%w: 文件格式或大小不受此模板支持", ledger.ErrInvalid)
	}
	records, err := readRecords(code, ext, data)
	if err != nil {
		return nil, fmt.Errorf("%w: 无法读取账单: %v", ledger.ErrInvalid, err)
	}
	start, header, err := dataStart(code, records)
	if err != nil {
		return nil, err
	}
	temp, err := os.MkdirTemp("", "wubu-statement-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if e := os.RemoveAll(temp); e != nil && err == nil {
			out = nil
			err = fmt.Errorf("清理账单临时文件失败: %w", e)
		}
	}()
	out = []Row{}
	for i := start; i < len(records); i++ {
		rec := records[i]
		if strings.TrimSpace(strings.Join(rec.values, "")) == "" {
			continue
		}
		first := strings.TrimSpace(rec.values[0])
		if strings.HasPrefix(first, "---") || strings.HasPrefix(first, "说明") || strings.HasPrefix(first, "以上数据") || strings.HasPrefix(first, "具体内容") || first == "合计" {
			break
		}
		row := Row{Number: rec.number, Values: rec.values, RawRecord: rec.raw, Header: header}
		if code == "wechat" || code == "alipay" {
			row.Order, err = paymentRow(code, header, rec.values, temp)
		} else {
			row.Order, err = bankRow(code, ext, records[:start], rec, temp)
		}
		if err != nil {
			row.Error = err.Error()
		} else if row.Order == nil {
			row.Error = "上游未形成资金记录，保留证据待核对"
		}
		out = append(out, row)
		if len(out) > MaxRows {
			return nil, fmt.Errorf("%w: 每份账单最多%d行", ledger.ErrInvalid, MaxRows)
		}
	}
	return out, nil
}

func readRecords(code, ext string, data []byte) ([]record, error) {
	out := []record{}
	if ext == ".xlsx" {
		f, e := excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: 32 << 20, UnzipXMLSizeLimit: 16 << 20})
		if e != nil {
			return nil, e
		}
		defer f.Close()
		names := f.GetSheetList()
		if len(names) == 0 {
			return nil, fmt.Errorf("没有工作表")
		}
		rows, e := f.GetRows(names[0])
		if e != nil {
			return nil, e
		}
		for i, r := range rows {
			b, _ := json.Marshal(r)
			out = append(out, record{r, string(b), i + 1})
		}
		return out, nil
	}
	if ext == ".xls" {
		f, e := xls.OpenReader(bytes.NewReader(data))
		if e != nil {
			return nil, e
		}
		sheet, e := f.GetSheet(0)
		if e != nil {
			return nil, e
		}
		for i := 0; i <= int(sheet.GetNumberRows()); i++ {
			r, e := sheet.GetRow(i)
			if e != nil || r == nil {
				continue
			}
			values := []string{}
			for _, c := range r.GetCols() {
				values = append(values, c.GetString())
			}
			b, _ := json.Marshal(values)
			out = append(out, record{values, string(b), i + 1})
		}
		return out, nil
	}
	if !utf8.Valid(data) {
		var e error
		data, e = simplifiedchinese.GB18030.NewDecoder().Bytes(data)
		if e != nil {
			return nil, e
		}
	}
	text := strings.TrimPrefix(string(data), "\ufeff")
	parseText := text
	offsets := []int{}
	if code == "hsbchk" {
		// This provider's native exports put tabs after quoted cells. Keep an
		// offset map so its tab-tolerant parser does not alter original evidence.
		var clean strings.Builder
		for i := 0; i < len(text); i++ {
			if text[i] != '\t' {
				offsets = append(offsets, i)
				clean.WriteByte(text[i])
			}
		}
		offsets = append(offsets, len(text))
		parseText = clean.String()
	}
	reader := csv.NewReader(strings.NewReader(parseText))
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	for {
		start := reader.InputOffset()
		r, e := reader.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		line, _ := reader.FieldPos(0)
		end := reader.InputOffset()
		if len(offsets) > 0 {
			start = int64(offsets[start])
			end = int64(offsets[end])
		}
		out = append(out, record{r, text[start:end], line})
		if len(out) > MaxRows+200 {
			return nil, fmt.Errorf("账单行数过多")
		}
	}
	return out, nil
}

func dataStart(code string, rows []record) (int, []string, error) {
	if code == "td" {
		return 0, []string{}, nil
	}
	for i, r := range rows {
		if len(r.values) == 0 {
			continue
		}
		h := strings.TrimSpace(r.values[0])
		joined := strings.Join(r.values, ",")
		found := false
		switch code {
		case "wechat":
			found = h == "交易时间" && strings.Contains(joined, "交易类型") && strings.Contains(joined, "商品") && strings.Contains(joined, "支付方式")
		case "alipay":
			found = strings.Contains(joined, "交易时间") && strings.Contains(joined, "商品说明") && strings.Contains(joined, "交易订单号") && strings.Contains(joined, "交易分类")
		case "bmo":
			found = h == "Item #" || h == "First Bank Card"
		case "boc":
			found = h == "交易日" || h == "记账日期"
		case "bocom_debit":
			found = h == "序号" && strings.Contains(joined, "借贷")
		case "ccb":
			found = h == "记账日" || h == "记账日期"
		case "cmb":
			found = h == "记账日期" || h == "交易日"
		case "hsbchk":
			found = h == "Date" || h == "Transaction date"
		case "cib_debit":
			found = h == "交易时间"
		case "spdb_debit":
			found = strings.Contains(joined, "交易日期") || strings.Contains(joined, "交易时间")
		case "citic":
			found = i == 1
		default:
			found = h == "交易日期"
		}
		if found {
			return i + 1, r.values, nil
		}
	}
	return 0, nil, fmt.Errorf("%w: 文件不符合所选平台或银行模板的原生表头", ledger.ErrInvalid)
}

func paymentRow(code string, header, values []string, temp string) (*ir.Order, error) {
	get := func(names ...string) string {
		for _, name := range names {
			for i, h := range header {
				if strings.TrimSpace(h) == name && i < len(values) {
					return strings.TrimSpace(values[i])
				}
			}
		}
		return ""
	}
	at := get("交易时间")
	parsed, e := nativeTime(at)
	if e != nil {
		return nil, e
	}
	at = parsed.Format("2006-01-02 15:04:05")
	amount := get("金额(元)", "金额（元）", "金额")
	amount = strings.NewReplacer("¥", "", "￥", "", ",", "", "元", "").Replace(strings.TrimSpace(amount))
	// Exact cents are checked before upstream's floating-point representation is used.
	var cents ledger.Money
	if e = json.Unmarshal([]byte(amount), &cents); e != nil || cents <= 0 {
		return nil, fmt.Errorf("金额无效")
	}
	var canonical []string
	var p provider.Interface
	pad := 17
	if code == "wechat" {
		canonical = []string{at, get("交易类型"), get("交易对方"), get("商品"), get("收/支"), amount, get("支付方式"), get("当前状态", "交易状态"), get("交易单号"), get("商户单号"), get("备注")}
		w := wechat.New()
		w.IgnoreInvalidTxTypes = true
		p = w
	} else {
		canonical = []string{at, get("交易分类"), get("交易对方"), get("对方账号"), get("商品说明"), get("收/支"), amount, get("收/付款方式", "收付款方式"), get("交易状态"), get("交易订单号"), get("商家订单号"), get("备注")}
		a := alipay.New()
		a.Config = &alipay.Config{KeepRefundRecords: true}
		p = a
		pad = 22
	}
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	for i := 0; i < pad; i++ {
		_ = w.Write([]string{"Wubu provider envelope"})
	}
	_ = w.Write(canonical)
	w.Flush()
	data := b.Bytes()
	if code == "alipay" {
		data, e = simplifiedchinese.GBK.NewEncoder().Bytes(data)
		if e != nil {
			return nil, e
		}
	}
	path := filepath.Join(temp, "row.csv")
	if e = os.WriteFile(path, data, 0600); e != nil {
		return nil, e
	}
	result, e := translate(p, path)
	if e != nil {
		return nil, e
	}
	if len(result.Orders) != 1 {
		return nil, nil
	}
	order := result.Orders[0]
	order.Money = float64(cents) / 100
	order.Currency = "CNY"
	return &order, nil
}

func nativeTime(value string) (time.Time, error) {
	zone := time.FixedZone("Asia/Shanghai", 8*3600)
	for _, layout := range []string{"2006-01-02 15:04:05", "2006/1/2 15:04:05", "2006/1/2 15:04", "2006-01-02 15:04", time.RFC3339Nano} {
		if t, e := time.ParseInLocation(layout, value, zone); e == nil {
			return t.In(zone), nil
		}
	}
	if serial, e := strconv.ParseFloat(value, 64); e == nil && serial > 20000 && serial < 100000 {
		t, e := excelize.ExcelDateToTime(serial, false)
		if e != nil {
			return t, e
		}
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), zone), nil
	}
	return time.Time{}, fmt.Errorf("交易时间无效")
}

// An upstream parser panic is an invalid external record, never permission to lose its evidence.
func translate(p provider.Interface, path string) (out *ir.IR, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("上游无法读取此记录: %v", failure)
		}
	}()
	return p.Translate(path)
}

func bankRow(code, ext string, prefix []record, row record, temp string) (*ir.Order, error) {
	if code == "spdb_debit" {
		p := spdb_debit.New()
		result, e := p.TranslateRecord(append([]string{}, row.values...))
		if e != nil {
			return nil, e
		}
		if len(result.Orders) != 1 {
			return nil, nil
		}
		return &result.Orders[0], nil
	}
	if code == "citic" {
		if len(row.values) < 8 {
			return nil, fmt.Errorf("中信记录列数不足")
		}
		p := citic.New()
		values := append([]string{}, row.values...)
		if e := p.TranslateToOrders(values); e != nil {
			return nil, e
		}
		o := p.Orders[0]
		kind := ir.TypeUnknown
		if o.Type == citic.OrderTypeSend {
			kind = ir.TypeSend
		} else if o.Type == citic.OrderTypeRecv {
			kind = ir.TypeRecv
		}
		return &ir.Order{Peer: "CITIC", Item: o.TradeDesc, Method: o.Method, PayTime: o.TradeTime, Money: o.Money, Type: kind, Currency: o.Currency}, nil
	}
	p, e := provider.New(code)
	if e != nil {
		return nil, e
	}
	records := append(append([]record{}, prefix...), row)
	if code == "ccb" {
		records = []record{}
		for i := 0; i < 6; i++ {
			records = append(records, record{values: []string{"Wubu metadata envelope"}})
		}
		records = append(records, row)
	}
	path := filepath.Join(temp, "row.csv")
	if code == "cib_debit" {
		f := excelize.NewFile()
		for i, r := range records {
			for j, v := range r.values {
				cell, _ := excelize.CoordinatesToCellName(j+1, i+1)
				_ = f.SetCellStr("Sheet1", cell, v)
			}
		}
		path = filepath.Join(temp, "row.xlsx")
		e = f.SaveAs(path)
		_ = f.Close()
	} else {
		var b bytes.Buffer
		w := csv.NewWriter(&b)
		for _, r := range records {
			_ = w.Write(r.values)
		}
		w.Flush()
		e = os.WriteFile(path, b.Bytes(), 0600)
	}
	if e != nil {
		return nil, e
	}
	result, e := translate(p, path)
	if e != nil {
		return nil, e
	}
	if len(result.Orders) != 1 {
		return nil, nil
	}
	order := result.Orders[0]
	if math.IsNaN(order.Money) || math.IsInf(order.Money, 0) {
		return nil, fmt.Errorf("金额无效")
	}
	return &order, nil
}
