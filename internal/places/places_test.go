package places

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

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

func TestCitySuggestionsFoldCaseAndAccentsAndPreferExactNames(t *testing.T) {
	ctx := t.Context()
	got, err := FindCities(ctx, "  SÃO PAULO  ", 5)
	if err != nil || len(got) == 0 || searchKey(got[0].Name) != "sao paulo" || got[0].Country == "" {
		t.Fatalf("accented city: %+v, %v", got, err)
	}
	plain, err := FindCities(ctx, "sao paulo", 5)
	if err != nil || !reflect.DeepEqual(got, plain) {
		t.Fatalf("accent folding: %+v, %v", plain, err)
	}
	got, err = FindCities(ctx, "London", 3)
	if err != nil || len(got) != 3 || searchKey(got[0].Name) != "london" {
		t.Fatalf("exact ranking: %+v, %v", got, err)
	}
	got, err = FindCities(ctx, "", 0)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty query: %+v, %v", got, err)
	}
	got, err = FindCities(ctx, "zzzz-no-city", 50)
	if err != nil || len(got) != 0 {
		t.Fatalf("unknown query: %+v, %v", got, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := FindCities(cancelled, "London", 10); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func BenchmarkCitySuggestions(b *testing.B) {
	_, err := FindCities(b.Context(), "London", 20)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		if _, err := FindCities(b.Context(), "sao", 20); err != nil {
			b.Fatal(err)
		}
	}
}
