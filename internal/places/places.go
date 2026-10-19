// Package places resolves GPS points to nearby cities using bundled GeoNames
// data. It never calls a network service.
package places

import (
	"bufio"
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	_ "embed"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"lumen/internal/core"
)

// City is the nearest populated place in the offline index.
type City struct {
	ID      int64
	Name    string
	Region  string
	Country string
	Lat     float64
	Lon     float64
}

//go:embed cities.tsv.gz
var compressed []byte

var index struct {
	once   sync.Once
	cells  map[[2]int][]City
	cities []searchableCity
	err    error
}

func load() {
	index.cells = make(map[[2]int][]City)
	z, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		index.err = err
		return
	}
	defer z.Close()
	r := csv.NewReader(bufio.NewReader(z))
	r.Comma = '\t'
	for {
		row, err := r.Read()
		if err == io.EOF {
			slices.SortFunc(index.cities, func(a, b searchableCity) int {
				if order := cmp.Compare(a.key, b.key); order != 0 {
					return order
				}
				return cmp.Compare(a.city.ID, b.city.ID)
			})
			return
		}
		if err != nil {
			index.err = err
			return
		}
		if len(row) != 6 {
			index.err = fmt.Errorf("city row has %d columns", len(row))
			return
		}
		id, e1 := strconv.ParseInt(row[0], 10, 64)
		lat, e2 := strconv.ParseFloat(row[2], 64)
		lon, e3 := strconv.ParseFloat(row[3], 64)
		if e1 != nil || e2 != nil || e3 != nil {
			index.err = fmt.Errorf("invalid city row %q", row[0])
			return
		}
		city := City{ID: id, Name: row[1], Lat: lat, Lon: lon, Region: row[4], Country: row[5]}
		key := [2]int{int(math.Floor(lat)), int(math.Floor(lon))}
		index.cells[key] = append(index.cells[key], city)
		index.cities = append(index.cities, searchableCity{city: city, key: searchKey(city.Name)})
	}
}

type searchableCity struct {
	city City
	key  string
}

// searchKey extends the common case-folding rule only for city discovery:
// accent-insensitive suggestions must not change name or path identities.
func searchKey(s string) string {
	return core.Fold(strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, norm.NFD.String(strings.TrimSpace(s))))
}

// FindCities searches immutable pre-folded names. Exact and prefix matches
// precede substring matches. The reduced catalogue has no population column;
// ties use name, country, region and GeoNames ID for stable suggestions.
func FindCities(ctx context.Context, query string, limit int) ([]core.CityMatch, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key := searchKey(query)
	if key == "" {
		return []core.CityMatch{}, nil
	}
	if limit == 0 {
		limit = 20
	}
	limit = min(max(limit, 1), 50)
	index.once.Do(load)
	if index.err != nil {
		return nil, index.err
	}
	start, _ := slices.BinarySearchFunc(index.cities, key, func(c searchableCity, key string) int {
		return cmp.Compare(c.key, key)
	})
	var matches []searchableCity
	for i := start; i < len(index.cities) && strings.HasPrefix(index.cities[i].key, key); i++ {
		matches = append(matches, index.cities[i])
	}
	if len(matches) < limit {
		for i, city := range index.cities {
			if i%256 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			if !strings.HasPrefix(city.key, key) && strings.Contains(city.key, key) {
				matches = append(matches, city)
			}
		}
	}
	rank := func(s string) int {
		if s == key {
			return 0
		}
		if strings.HasPrefix(s, key) {
			return 1
		}
		return 2
	}
	slices.SortFunc(matches, func(a, b searchableCity) int {
		if order := cmp.Compare(rank(a.key), rank(b.key)); order != 0 {
			return order
		}
		for _, pair := range [][2]string{{a.city.Name, b.city.Name}, {a.city.Country, b.city.Country}, {a.city.Region, b.city.Region}} {
			if order := core.CompareNames(pair[0], pair[1]); order != 0 {
				return order
			}
		}
		return cmp.Compare(a.city.ID, b.city.ID)
	})
	out := make([]core.CityMatch, 0, min(limit, len(matches)))
	for _, match := range matches[:min(limit, len(matches))] {
		city := match.city
		out = append(out, core.CityMatch{ID: city.ID, Name: city.Name, Region: city.Region, Country: city.Country, Lat: city.Lat, Lon: city.Lon})
	}
	return out, ctx.Err()
}

// Lookup returns the nearest city within 75 km. Remote GPS points stay
// unlabelled rather than being assigned a misleading distant city.
func Lookup(lat, lon float64) (City, bool, error) {
	if math.IsNaN(lat) || math.IsNaN(lon) || lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return City{}, false, nil
	}
	index.once.Do(load)
	if index.err != nil {
		return City{}, false, index.err
	}
	const radius = 75.0
	latCell := int(math.Floor(lat))
	lonCell := int(math.Floor(lon))
	lonRange := int(math.Ceil(radius/(111*math.Max(0.01, math.Cos(lat*math.Pi/180))))) + 1
	if lonRange > 180 {
		lonRange = 180
	}
	best := City{}
	bestDistance := radius
	for y := latCell - 1; y <= latCell+1; y++ {
		for x := lonCell - lonRange; x <= lonCell+lonRange; x++ {
			wrapped := (x + 180) % 360
			if wrapped < 0 {
				wrapped += 360
			}
			wrapped -= 180
			for _, city := range index.cells[[2]int{y, wrapped}] {
				if d := distance(lat, lon, city.Lat, city.Lon); d < bestDistance {
					best, bestDistance = city, d
				}
			}
		}
	}
	return best, best.ID != 0, nil
}

// distance is great-circle distance in kilometres.
func distance(lat1, lon1, lat2, lon2 float64) float64 {
	a := math.Sin((lat2 - lat1) * math.Pi / 360)
	b := math.Sin((lon2 - lon1) * math.Pi / 360)
	h := a*a + math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*b*b
	return 2 * 6371 * math.Asin(math.Sqrt(math.Min(1, h)))
}
