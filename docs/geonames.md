# Offline place data

Lumen includes a reduced snapshot of [GeoNames](https://www.geonames.org/) data,
licensed under [Creative Commons Attribution 4.0](https://creativecommons.org/licenses/by/4.0/).
Credit: GeoNames contributors. GeoNames provides the data as is. Lumen never
contacts GeoNames or another geocoding service at run time.

The 30 September 2026 snapshot contains 69,760 populated places from
`cities5000.zip`, plus English region names from `admin1CodesASCII.txt` and
country names from `countryInfo.txt`. `scripts/build_places.py` checks all
three downloaded files by SHA-256 and writes the deterministic compressed
`internal/places/cities.tsv.gz`. The script omits alternate names, population,
timezone and most other source fields. The source files are available at
`https://download.geonames.org/export/dump/`.

The lookup chooses the nearest city within 75 km. It is an approximate label:
in rural areas or near a border it can be absent or show a nearby city in a
different administrative area. Lumen retains the original GPS coordinates;
it never changes the file's metadata.
