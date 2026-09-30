package places

import "testing"

func TestLookupOfflineCitiesAndRemotePoint(t *testing.T) {
	for _, tc := range []struct {
		lat, lon float64
		want     string
	}{
		{31.5546, 74.3572, "Lahore"},
		{51.5074, -0.1278, "London"},
	} {
		city, ok, err := Lookup(tc.lat, tc.lon)
		if err != nil || !ok || city.Name != tc.want || city.Country == "" {
			t.Errorf("Lookup(%v, %v) = %+v, %t, %v; want %s", tc.lat, tc.lon, city, ok, err, tc.want)
		}
	}
	if city, ok, err := Lookup(0, 0); err != nil || ok || city.ID != 0 {
		t.Errorf("remote point = %+v, %t, %v", city, ok, err)
	}
}
