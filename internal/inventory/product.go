package inventory

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// ProductSpec describes a package, independently of the stock counting unit.
type ProductSpec struct {
	Spec        string   `json:"spec,omitempty"`
	PackageSize *float64 `json:"package_size,omitempty"`
	PackageUnit string   `json:"package_unit,omitempty"`
}

var packagePattern = regexp.MustCompile(`(?i)([0-9]+(?:\.[0-9]+)?)\s*(ml|kg|l|g|毫升|千克|公斤|升|克)\s*$`)

func PlainText(s string) string {
	s = strings.Map(func(r rune) rune {
		if r >= 0xff01 && r <= 0xff5e {
			return r - 0xfee0
		}
		if r == 0x3000 {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

func NormalizeProduct(name string, p ProductSpec) (string, ProductSpec, error) {
	name, p.Spec = PlainText(name), PlainText(p.Spec)
	// Accept legacy combined names while storing their dimensions separately.
	if loc := packagePattern.FindStringSubmatchIndex(name); loc != nil && loc[0] > 0 {
		inline := name[loc[0]:]
		if p.Spec != "" && SpecKey(p.Spec) != SpecKey(inline) {
			return name, p, fmt.Errorf("商品名称中的规格与 spec 不一致")
		}
		if p.Spec == "" {
			p.Spec = inline
		}
		name = strings.TrimSpace(name[:loc[0]])
	}
	if p.PackageSize != nil {
		if math.IsNaN(*p.PackageSize) || math.IsInf(*p.PackageSize, 0) || *p.PackageSize <= 0 || *p.PackageSize > MaxQuantity || len([]rune(p.PackageUnit)) > MaxUnitLength || strings.TrimSpace(p.PackageUnit) == "" {
			return name, p, fmt.Errorf("包装规格需要正数 package_size 和 package_unit")
		}
		structured := strconv.FormatFloat(*p.PackageSize, 'f', -1, 64) + canonicalPackageUnit(p.PackageUnit)
		if p.Spec != "" && SpecKey(p.Spec) != SpecKey(structured) {
			return name, p, fmt.Errorf("spec 与包装规格字段不一致")
		}
		p.Spec = structured
	} else if p.PackageUnit != "" {
		return name, p, fmt.Errorf("package_unit 需要 package_size")
	}
	if m := packagePattern.FindStringSubmatch(p.Spec); m != nil && m[0] == p.Spec {
		size, err := strconv.ParseFloat(m[1], 64)
		if err != nil || size <= 0 || math.IsInf(size, 0) || size > MaxQuantity {
			return name, p, fmt.Errorf("包装规格超出范围")
		}
		p.PackageSize, p.PackageUnit = &size, canonicalPackageUnit(m[2])
		p.Spec = strconv.FormatFloat(size, 'f', -1, 64) + p.PackageUnit
	}
	if len([]rune(p.Spec)) > 100 {
		return name, p, fmt.Errorf("规格最多 100 个字符")
	}
	return name, p, nil
}

func canonicalPackageUnit(s string) string {
	switch strings.ToLower(PlainText(s)) {
	case "l", "升":
		return "L"
	case "ml", "毫升":
		return "ml"
	case "kg", "千克", "公斤":
		return "kg"
	case "g", "克":
		return "g"
	default:
		return PlainText(s)
	}
}

func NameKey(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, PlainText(s))
}

// Only compatible volume/mass specifications convert; inventory counts never do.
func SpecKey(s string) string {
	s = PlainText(s)
	if m := packagePattern.FindStringSubmatch(s); m != nil && m[0] == s {
		v, _ := strconv.ParseFloat(m[1], 64)
		u := canonicalPackageUnit(m[2])
		if u == "L" {
			v *= 1000
			u = "ml"
		}
		if u == "kg" {
			v *= 1000
			u = "g"
		}
		return strconv.FormatFloat(v, 'f', -1, 64) + u
	}
	return NameKey(s)
}

func UnitKey(s string) string {
	switch strings.ToLower(PlainText(s)) {
	case "l", "升":
		return "升"
	case "ml", "毫升":
		return "毫升"
	case "bottle", "bottles", "瓶":
		return "瓶"
	case "can", "cans", "罐":
		return "罐"
	case "pcs", "pc", "piece", "pieces", "个":
		return "个"
	case "g", "克":
		return "克"
	case "kg", "千克", "公斤":
		return "千克"
	default:
		return NameKey(s)
	}
}
