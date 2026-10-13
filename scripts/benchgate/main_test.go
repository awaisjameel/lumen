package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

const testBenchmark = "BenchmarkLargeLibraryStore/none/Things"

func TestGateRejectsRegressionsAndBudgetDrift(t *testing.T) {
	for _, tc := range []struct {
		name, budgets, results, want string
	}{
		{"exceeded", testBenchmark + " 1ms", testBenchmark + "-20 3 2000000 ns/op", "2.000 ms/op exceeds budget 1.000 ms/op"},
		{"missing", testBenchmark + " 1ms", testBenchmark + "-20 3 1 ns/op\nBenchmarkLargeLibraryStore/none/People-20 3 2 ns/op", "People: 0.000 ms/op has no budget"},
		{"unused", testBenchmark + " 1ms\nBenchmarkLargeLibraryStore/none/People 1s", testBenchmark + "-20 3 1 ns/op", "People: budget 1000.000 ms/op has no benchmark"},
		{"empty results", testBenchmark + " 1ms", "PASS\nok lumen/internal/store", "no benchmark results"},
		{"empty budgets", "# empty", testBenchmark + "-20 3 1 ns/op", "no budgets"},
		{"malformed budgets", testBenchmark + " bad", testBenchmark + "-20 3 1 ns/op", "invalid positive duration"},
		{"malformed results", testBenchmark + " 1ms", testBenchmark + "-20 3 0 ns/op", "invalid positive figure"},
		{"slowest sample", testBenchmark + " 1ms", testBenchmark + "-20 3 2 ms/op\n" + testBenchmark + "-20 3 1 ns/op", "exceeds budget"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var table bytes.Buffer
			err := check(strings.NewReader(tc.budgets), strings.NewReader(tc.results), &table)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("check = %v, want %q", err, tc.want)
			}
			if !strings.Contains(table.String(), "headroom") {
				t.Fatal("comparison must print its table even on failure")
			}
			if tc.name == "empty results" && !strings.Contains(err.Error(), testBenchmark+": budget 1.000 ms/op has no benchmark") {
				t.Fatal("an empty result must name every unused budget")
			}
			if tc.name == "empty budgets" && !strings.Contains(err.Error(), testBenchmark+": 0.000 ms/op has no budget") {
				t.Fatal("an empty budget file must name every unbudgeted benchmark")
			}
		})
	}
}

func TestGateAcceptsEqualCeilingsAndConvertsUnits(t *testing.T) {
	for _, unit := range []string{"ns", "us", "µs", "ms", "s"} {
		t.Run(unit, func(t *testing.T) {
			var table bytes.Buffer
			budgets := "# a reviewed ceiling\n\n" + testBenchmark + " 2" + unit + " # repeat measurements\n"
			results := "goos: windows\nPASS\n" + testBenchmark + "-8 3 2 " + unit + "/op\n"
			if err := check(strings.NewReader(budgets), strings.NewReader(results), &table); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(table.String(), "0%") || !strings.Contains(table.String(), testBenchmark) {
				t.Fatalf("table does not report equal ceiling: %s", table.String())
			}
		})
	}
}

func TestGateRejectsMalformedBudgets(t *testing.T) {
	for _, line := range []string{
		testBenchmark, testBenchmark + " 1ms extra", "wrong 1ms",
		testBenchmark + " bad", testBenchmark + " 0s", testBenchmark + " -1ms",
		testBenchmark + " 1ms\n" + testBenchmark + " 2ms",
	} {
		t.Run(line, func(t *testing.T) {
			if _, err := readBudgets(strings.NewReader(line)); err == nil {
				t.Fatalf("accepted malformed budget %q", line)
			}
		})
	}
}

func TestGateRejectsMalformedResults(t *testing.T) {
	for _, suffix := range []string{
		"", " 3", " 3 1", " 3 1 ns/op extra", " zero 1 ns/op", " 0 1 ns/op",
		" 3 bad ns/op", " 3 NaN ns/op", " 3 +Inf ns/op", " 3 0 ns/op", " 3 -1 ns/op",
		" 3 1 MB/op", " 3 1 ns", " 3 1e309 ns/op", " 3 1e308 s/op",
	} {
		t.Run(suffix, func(t *testing.T) {
			if _, err := readResults(strings.NewReader(testBenchmark + "-20" + suffix)); err == nil {
				t.Fatalf("accepted malformed result %q", suffix)
			}
		})
	}
}

type failedReader struct{}

func (failedReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestGateReturnsInputAndOutputFailures(t *testing.T) {
	if _, err := readBudgets(failedReader{}); err == nil {
		t.Fatal("budget reader failure was lost")
	}
	if _, err := readResults(failedReader{}); err == nil {
		t.Fatal("result reader failure was lost")
	}
	if err := check(strings.NewReader(testBenchmark+" 1ms"), strings.NewReader(testBenchmark+" 3 1 ns/op"), failedWriter{}); err == nil {
		t.Fatal("table writer failure was lost")
	}
}
