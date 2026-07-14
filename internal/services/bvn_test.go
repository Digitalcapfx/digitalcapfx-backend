package services

import "testing"

func TestIsNigerianCustomer(t *testing.T) {
	cases := []struct {
		country string
		phone   string // already normalized (E.164)
		want    bool
	}{
		{"NG", "+237600000000", true},        // explicit country wins
		{"Nigeria", "", true},                // name form
		{"NGA", "", true},                    // alpha-3
		{"ng", "", true},                     // case-insensitive
		{"", "+2348012345678", true},         // Nigerian phone, no country
		{"CM", "+237600000000", false},       // Cameroon
		{"", "+237600000000", false},         // Cameroon phone
		{"", "", false},                      // nothing
		{"GB", "+2348012345678", true},       // Nigerian phone still triggers
	}
	for _, c := range cases {
		if got := isNigerianCustomer(c.country, c.phone); got != c.want {
			t.Errorf("isNigerianCustomer(%q,%q) = %v, want %v", c.country, c.phone, got, c.want)
		}
	}
}
