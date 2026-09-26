// Spike: load sqlite-vec into mattn/go-sqlite3 and time KNN search over
// 300k x 768-dim vectors (the plan's target library), float32 vs int8, with
// and without a date filter, against a plain in-memory Go scan.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

const dim = 768

func main() {
	n := flag.Int("n", 300_000, "vectors")
	dir := flag.String("dir", "out/vec", "database folder")
	queries := flag.Int("q", 20, "queries per measurement")
	flag.Parse()
	sqlite_vec.Auto()
	os.MkdirAll(*dir, 0o755)
	dbPath := filepath.Join(*dir, fmt.Sprintf("vec-%d.db", *n))

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=-262144")
	check(err)
	defer db.Close()
	var ver string
	check(db.QueryRow("select vec_version()").Scan(&ver))
	fmt.Println("sqlite-vec", ver, "·", *n, "vectors ·", dim, "dims")

	// Clustered synthetic data: real embeddings are lumpy, not uniform noise.
	rng := rand.New(rand.NewPCG(1, 2))
	centres := make([][]float32, 200)
	for i := range centres {
		centres[i] = randUnit(rng, nil, 1)
	}
	vecs := make([][]float32, *n)
	for i := range vecs {
		vecs[i] = randUnit(rng, centres[rng.IntN(len(centres))], 0.6)
	}
	scale := int8Scale(vecs)

	var have int
	db.QueryRow("select count(*) from clip_f32").Scan(&have)
	if have != *n {
		build(db, vecs, scale)
	} else {
		fmt.Println("reusing", dbPath)
	}
	if st, err := os.Stat(dbPath); err == nil {
		fmt.Printf("database size: %.0f MB\n", float64(st.Size())/1e6)
	}

	qs := make([][]float32, *queries)
	for i := range qs {
		qs[i] = randUnit(rng, centres[rng.IntN(len(centres))], 0.6)
	}

	// Ground truth from an exact in-memory scan.
	truth := make([][]int64, len(qs))
	measure("Go in-memory scan, float32, all cores", qs, func(i int, q []float32) {
		truth[i] = goScan(vecs, q, 50)
	})

	f32 := func(where string) func(int, []float32) {
		return func(i int, q []float32) {
			ids := knn(db, "select asset_id from clip_f32 where embedding match ? and k = 50"+where, f32blob(q))
			recall[i] = overlap(ids, truth[i])
		}
	}
	i8 := func(where string) func(int, []float32) {
		return func(i int, q []float32) {
			ids := knn(db, "select asset_id from clip_i8 where embedding match vec_int8(?) and k = 50"+where, i8blob(q, scale))
			recall[i] = overlap(ids, truth[i])
		}
	}
	// The same scan over int8 copies: 4x less memory (300k x 768 = 230 MB).
	q8 := make([][]int8, len(vecs))
	for i, v := range vecs {
		q8[i] = quant(v, scale)
	}
	recall = make([]float64, len(qs))
	measure("Go in-memory scan, int8, all cores", qs, func(i int, q []float32) {
		recall[i] = overlap(goScan8(q8, quant(q, scale), 50), truth[i])
	})
	report()
	measure("sqlite-vec float32", qs, f32(""))
	report()
	measure("sqlite-vec int8", qs, i8(""))
	report()
	// Metadata column filter: roughly one year out of ~20.
	recall = make([]float64, len(qs))
	measure("sqlite-vec float32 + year filter", qs, f32(" and taken_year = 2021"))
	measure("sqlite-vec int8 + year filter", qs, i8(" and taken_year = 2021"))
}

var recall []float64

func report() {
	s := 0.0
	for _, r := range recall {
		s += r
	}
	fmt.Printf("    recall@50 vs exact: %.3f\n", s/float64(len(recall)))
	recall = make([]float64, len(recall))
}

func build(db *sql.DB, vecs [][]float32, scale float32) {
	for _, s := range []string{
		"drop table if exists clip_f32", "drop table if exists clip_i8",
		fmt.Sprintf("create virtual table clip_f32 using vec0(asset_id integer primary key, taken_year integer, embedding float[%d] distance_metric=cosine)", dim),
		fmt.Sprintf("create virtual table clip_i8 using vec0(asset_id integer primary key, taken_year integer, embedding int8[%d] distance_metric=cosine)", dim),
	} {
		_, err := db.Exec(s)
		check(err)
	}
	start := time.Now()
	const batch = 1000
	for lo := 0; lo < len(vecs); lo += batch {
		tx, err := db.Begin()
		check(err)
		a, err := tx.Prepare("insert into clip_f32(asset_id, taken_year, embedding) values (?, ?, ?)")
		check(err)
		b, err := tx.Prepare("insert into clip_i8(asset_id, taken_year, embedding) values (?, ?, vec_int8(?))")
		check(err)
		for i := lo; i < min(lo+batch, len(vecs)); i++ {
			year := 2006 + i%20
			_, err = a.Exec(i+1, year, f32blob(vecs[i]))
			check(err)
			_, err = b.Exec(i+1, year, i8blob(vecs[i], scale))
			check(err)
		}
		check(tx.Commit())
	}
	d := time.Since(start)
	fmt.Printf("insert (both tables): %.1fs = %.0f vectors/s\n", d.Seconds(), float64(len(vecs))/d.Seconds())
}

func knn(db *sql.DB, q string, arg []byte) []int64 {
	rows, err := db.Query(q, arg)
	check(err)
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		check(rows.Scan(&id))
		ids = append(ids, id)
	}
	check(rows.Err())
	return ids
}

func measure(name string, qs [][]float32, fn func(int, []float32)) {
	fn(0, qs[0]) // warm the page cache
	var ds []time.Duration
	for i, q := range qs {
		t := time.Now()
		fn(i, q)
		ds = append(ds, time.Since(t))
	}
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
	fmt.Printf("  %-40s median %6.1f ms   p95 %6.1f ms\n", name, ms(ds[len(ds)/2]), ms(ds[len(ds)*95/100]))
}

func goScan(vecs [][]float32, q []float32, k int) []int64 {
	type hit struct {
		id int64
		s  float32
	}
	workers := runtime.NumCPU()
	parts := make([][]hit, workers)
	var wg sync.WaitGroup
	chunk := (len(vecs) + workers - 1) / workers
	for w := range workers {
		wg.Go(func() {
			lo, hi := w*chunk, min((w+1)*chunk, len(vecs))
			var top []hit
			for i := lo; i < hi; i++ {
				var s float32
				v := vecs[i]
				for j := range v {
					s += v[j] * q[j]
				}
				if len(top) < k || s > top[len(top)-1].s {
					top = append(top, hit{int64(i + 1), s})
					sort.Slice(top, func(a, b int) bool { return top[a].s > top[b].s })
					if len(top) > k {
						top = top[:k]
					}
				}
			}
			parts[w] = top
		})
	}
	wg.Wait()
	var all []hit
	for _, p := range parts {
		all = append(all, p...)
	}
	sort.Slice(all, func(a, b int) bool { return all[a].s > all[b].s })
	ids := make([]int64, 0, k)
	for _, h := range all[:min(k, len(all))] {
		ids = append(ids, h.id)
	}
	return ids
}

// goScan8 is goScan over int8 vectors with int32 dot products.
func goScan8(vecs [][]int8, q []int8, k int) []int64 {
	type hit struct {
		id int64
		s  int32
	}
	workers := runtime.NumCPU()
	parts := make([][]hit, workers)
	var wg sync.WaitGroup
	chunk := (len(vecs) + workers - 1) / workers
	for w := range workers {
		wg.Go(func() {
			lo, hi := w*chunk, min((w+1)*chunk, len(vecs))
			var top []hit
			for i := lo; i < hi; i++ {
				var s int32
				v := vecs[i]
				for j := range v {
					s += int32(v[j]) * int32(q[j])
				}
				if len(top) < k || s > top[len(top)-1].s {
					top = append(top, hit{int64(i + 1), s})
					sort.Slice(top, func(a, b int) bool { return top[a].s > top[b].s })
					if len(top) > k {
						top = top[:k]
					}
				}
			}
			parts[w] = top
		})
	}
	wg.Wait()
	var all []hit
	for _, p := range parts {
		all = append(all, p...)
	}
	sort.Slice(all, func(a, b int) bool { return all[a].s > all[b].s })
	ids := make([]int64, 0, k)
	for _, h := range all[:min(k, len(all))] {
		ids = append(ids, h.id)
	}
	return ids
}

func quant(v []float32, scale float32) []int8 {
	out := make([]int8, len(v))
	for i, x := range v {
		out[i] = int8(max(-128, min(127, math.Round(float64(x*scale)))))
	}
	return out
}

func overlap(a, b []int64) float64 {
	set := map[int64]bool{}
	for _, x := range b {
		set[x] = true
	}
	n := 0
	for _, x := range a {
		if set[x] {
			n++
		}
	}
	return float64(n) / float64(max(1, len(b)))
}

// randUnit returns a random unit vector, near centre when one is given.
func randUnit(rng *rand.Rand, centre []float32, spread float64) []float32 {
	v := make([]float32, dim)
	var norm float64
	for i := range v {
		x := rng.NormFloat64() / math.Sqrt(dim) * spread
		if centre != nil {
			x += float64(centre[i])
		}
		v[i] = float32(x)
		norm += x * x
	}
	norm = math.Sqrt(norm)
	for i := range v {
		v[i] = float32(float64(v[i]) / norm)
	}
	return v
}

// int8Scale maps the largest component magnitude to 127. Cosine distance
// ignores a uniform scale, so one factor serves the whole collection.
func int8Scale(vecs [][]float32) float32 {
	var m float32
	for _, v := range vecs {
		for _, x := range v {
			m = max(m, float32(math.Abs(float64(x))))
		}
	}
	return 127 / m
}

func f32blob(v []float32) []byte {
	b, err := sqlite_vec.SerializeFloat32(v)
	check(err)
	return b
}

func i8blob(v []float32, scale float32) []byte {
	b := make([]byte, len(v))
	for i, x := range v {
		q := math.Round(float64(x * scale))
		b[i] = byte(int8(max(-128, min(127, q))))
	}
	return b
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func check(err error) {
	if err != nil {
		panic(err)
	}
}
