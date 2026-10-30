package model

import "testing"

func TestResolveDivision(t *testing.T) {
	cases := []struct {
		code     uint
		province string
		city     string
		district string
	}{
		{110000, "北京市", "北京市", ""},    // province-level code of a municipality
		{440000, "广东省", "", ""},       // province-level code
		{320100, "江苏省", "南京市", ""},    // prefecture-level code
		{440300, "广东省", "深圳市", ""},    // prefecture-level code
		{110105, "北京市", "北京市", "朝阳区"}, // county-level under a municipality
		{410105, "河南省", "郑州市", "金水区"},
		{440305, "广东省", "深圳市", "南山区"},
		{419001, "河南省", "济源市", ""}, // province-administered county-level city
		{0, "", "", ""},
		{999999, "", "", ""},
	}
	for _, c := range cases {
		if p, ci, d := ResolveDivision(c.code); p != c.province || ci != c.city || d != c.district {
			t.Errorf("ResolveDivision(%d) = %q, %q, %q; want %q, %q, %q",
				c.code, p, ci, d, c.province, c.city, c.district)
		}
	}
}

func TestDivisionTableLoaded(t *testing.T) {
	if len(divisionTable) < 2000 {
		t.Fatalf("division table looks truncated: %d entries", len(divisionTable))
	}
}
