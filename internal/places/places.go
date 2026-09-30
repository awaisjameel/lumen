// Package places resolves GPS points to nearby cities using bundled GeoNames
// data. It never calls a network service.
package places

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/csv"
	"fmt"
	"io"
	"math"
	"strconv"
	"sync"
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
	once  sync.Once
	cells map[[2]int][]City
	err   error
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
	}
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
