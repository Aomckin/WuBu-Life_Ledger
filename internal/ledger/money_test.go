package ledger

import (
	"encoding/json"
	"testing"
	"time"
)

func TestV02SignedMoneyAndPeriods(t *testing.T) {
	for _, c := range []struct {
		cents SignedMoney
		json  string
	}{{-8, "-0.08"}, {-12345, "-123.45"}, {0, "0.00"}, {12853, "128.53"}} {
		b, e := json.Marshal(c.cents)
		if e != nil || string(b) != c.json {
			t.Fatal(string(b), e)
		}
		var decoded SignedMoney
		if e = json.Unmarshal(b, &decoded); e != nil || decoded != c.cents {
			t.Fatal(decoded, e)
		}
	}
	for _, raw := range []string{"1.001", "-10000000000.01", "null", "\"1\""} {
		var n SignedMoney
		if e := json.Unmarshal([]byte(raw), &n); e == nil {
			t.Fatal("accepted invalid balance", raw)
		}
	}
	var amount Money
	if e := json.Unmarshal([]byte("-1"), &amount); e == nil {
		t.Fatal("absolute amount must remain positive")
	}
	now, _ := time.Parse(time.RFC3339, "2026-12-31T16:30:00Z")
	for _, test := range []struct{ period, start, end string }{{"today", "2026-12-31T16:00:00Z", "2027-01-01T16:00:00Z"}, {"week", "2026-12-27T16:00:00Z", "2027-01-03T16:00:00Z"}, {"month", "2026-12-31T16:00:00Z", "2027-01-31T16:00:00Z"}, {"year", "2026-12-31T16:00:00Z", "2027-12-31T16:00:00Z"}} {
		start, end, e := PeriodRange(test.period, "", "", now)
		if e != nil || start != test.start || end != test.end {
			t.Fatal(test, start, end, e)
		}
	}
	if _, _, e := PeriodRange("month", "2026-10-01T00:00:00Z", "", now); e == nil {
		t.Fatal("mixed period/custom range")
	}
	if _, _, e := PeriodRange("", "2026-10-02T00:00:00Z", "2026-10-01T00:00:00Z", now); e == nil {
		t.Fatal("inverted range")
	}
}
