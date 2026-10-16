package core

// OriginalDate is the metadata date underneath a local correction.
type OriginalDate struct {
	TakenAt int64      `json:"takenAt"` // wall-clock seconds, including dates before 1970
	From    DateSource `json:"from"`
}

// OriginalLocation is the metadata location underneath a local correction.
// Both coordinates are nil when the file supplies no location.
type OriginalLocation struct {
	Lat *float64 `json:"lat"`
	Lon *float64 `json:"lon"`
}

// CityMatch is a city in the bundled offline catalogue, including cities
// that have no photos yet. ID is its GeoNames identity, not an asset ID.
type CityMatch struct {
	ID      int64   `json:"id"`
	Name    string  `json:"name"`
	Region  string  `json:"region"`
	Country string  `json:"country"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
}

// LibraryCounts supplies sidebar totals and the Library caption. People
// counts named, unblocked people with visible photos. Duplicates are omitted:
// their page counts computed similarity groups, which cannot be read cheaply.
type LibraryCounts struct {
	Photos     int `json:"photos"`
	Videos     int `json:"videos"`
	Favourites int `json:"favourites"`
	People     int `json:"people"`
	Trash      int `json:"trash"`
}
