package tmdb

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TMDB fills production_countries for well-known titles and leaves it empty for
// many smaller ones, where origin_country is the only signal there is.
func TestCountryCodesFallsBackToOriginCountry(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "production countries present",
			body: `{"production_countries":[{"iso_3166_1":"US"},{"iso_3166_1":"GB"}],"origin_country":["US"]}`,
			want: []string{"US", "GB"},
		},
		{
			name: "only origin_country",
			body: `{"production_countries":[],"origin_country":["KZ"]}`,
			want: []string{"KZ"},
		},
		{
			name: "neither",
			body: `{"production_countries":[],"origin_country":[]}`,
			want: []string{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var d Details
			if err := json.Unmarshal([]byte(c.body), &d); err != nil {
				t.Fatal(err)
			}
			got := d.CountryCodes()
			if len(got) == 0 && len(c.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("CountryCodes() = %v, want %v", got, c.want)
			}
		})
	}
}
