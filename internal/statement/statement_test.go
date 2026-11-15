package statement

import (
	"bytes"
	"github.com/xuri/excelize/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPinnedProviderNativeFixtures(t *testing.T) {
	for _, template := range Templates {
		t.Run(template.ID, func(t *testing.T) {
			count := 0
			e := filepath.WalkDir(filepath.Join("testdata", template.ID), func(path string, d os.DirEntry, e error) error {
				if e != nil {
					return e
				}
				if d.IsDir() {
					return nil
				}
				data, e := os.ReadFile(path)
				if e != nil {
					return e
				}
				rows, e := Parse(template.ID, filepath.Base(path), data)
				if e != nil {
					t.Errorf("%s: %v", path, e)
					return nil
				}
				parsed := 0
				for _, r := range rows {
					if r.Order != nil && r.Error == "" {
						parsed++
					}
				}
				if parsed == 0 {
					t.Errorf("%s: no usable upstream orders; rows=%d first_error=%s", path, len(rows), func() string {
						if len(rows) > 0 {
							return rows[0].Error
						}
						return "no records"
					}())
				}
				t.Logf("%s: evidence=%d parsed=%d", path, len(rows), parsed)
				count++
				return nil
			})
			if e != nil {
				t.Fatal(e)
			}
			if count == 0 {
				t.Fatal("no native format fixture")
			}
		})
	}
}
func TestPaymentRowFailureIsolatedAndWrongEntryRejected(t *testing.T) {
	header := "交易时间,交易类型,交易对方,商品,收/支,金额(元),支付方式,当前状态,交易单号,商户单号,备注\n"
	text := header + "2026-11-01 12:03:00,商户消费,龙记,,支出,20.00,零钱,支付成功,one,,\n2026-11-01 12:04:00,商户消费,坏行,,支出,invalid,零钱,支付成功,two,,\n2026-11-01 12:05:00,商户消费,便利店,,支出,7.00,零钱,支付成功,three,,\n"
	rows, e := Parse("wechat", "wechat.csv", []byte(text))
	if e != nil || len(rows) != 3 || rows[0].Order == nil || rows[1].Error == "" || rows[2].Order == nil {
		t.Fatal(rows, e)
	}
	if _, e = Parse("alipay", "wechat.csv", []byte(text)); e == nil {
		t.Fatal("wrong provider accepted")
	}
	f := excelize.NewFile()
	all := strings.Split(strings.TrimSpace(text), "\n")
	for i, line := range all {
		for j, v := range strings.Split(line, ",") {
			cell, _ := excelize.CoordinatesToCellName(j+1, i+1)
			_ = f.SetCellStr("Sheet1", cell, v)
		}
	}
	b, e := f.WriteToBuffer()
	if e != nil {
		t.Fatal(e)
	}
	_ = f.Close()
	x, e := Parse("wechat", "微信原生.xlsx", b.Bytes())
	if e != nil || len(x) != 3 || x[0].Order == nil || x[1].Error == "" {
		t.Fatal(x, e)
	}
	if !bytes.Contains([]byte(x[0].RawRecord), []byte("龙记")) {
		t.Fatal("lost raw cells")
	}
}

func TestTemporaryEnvelopesAreRemoved(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TEMP", dir)
	t.Setenv("TMP", dir)
	t.Setenv("TMPDIR", dir)
	file, e := os.ReadFile(filepath.Join("testdata", "abc_debit", "example-abc_debit-records.csv"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Parse("abc_debit", "bank.csv", file); e != nil {
		t.Fatal(e)
	}
	remaining, e := os.ReadDir(dir)
	if e != nil || len(remaining) != 0 {
		t.Fatal("native temporary files retained", remaining, e)
	}
}

func TestNativeTimesUseShanghaiWallClock(t *testing.T) {
	for _, value := range []string{"2026-11-01T04:03:00Z", "2026/11/1 12:03"} {
		at, e := nativeTime(value)
		if e != nil || at.Format("2006-01-02 15:04:05") != "2026-11-01 12:03:00" {
			t.Fatal(at, e)
		}
	}
	at, e := nativeTime("46000.5")
	_, offset := at.Zone()
	if e != nil || at.Hour() != 12 || offset != 8*3600 {
		t.Fatal(at, e)
	}
}

func TestRegistryRetainsEveryUpstreamBank(t *testing.T) {
	nonBanks := map[string]bool{"wechat": true, "alipay": true, "htsec": true, "hxsec": true, "ibkr": true, "huobi": true, "oklink": true, "mt": true, "jd": true}
	dirs, e := os.ReadDir(filepath.Join("..", "..", "third_party", "double-entry-generator", "pkg", "provider"))
	if e != nil {
		t.Fatal(e)
	}
	for _, d := range dirs {
		if !d.IsDir() || nonBanks[d.Name()] {
			continue
		}
		template, ok := Lookup(d.Name())
		if !ok || template.Group != "bank" {
			t.Errorf("upstream bank %s missing from product registry", d.Name())
		}
	}
}
