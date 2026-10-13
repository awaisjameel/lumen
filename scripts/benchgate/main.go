// Checks large-library benchmark results against reviewed latency ceilings.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

func main() {
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: benchgate < RESULTS (uses internal/store/testdata/library-budgets.txt)")
		os.Exit(2)
	}
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(results io.Reader, out io.Writer) error {
	budgets, err := os.Open("internal/store/testdata/library-budgets.txt")
	if err != nil {
		return errors.Join(err, check(strings.NewReader(""), results, out))
	}
	defer func() { _ = budgets.Close() }()
	return check(budgets, results, out)
}

func check(budgetReader, resultReader io.Reader, out io.Writer) error {
	budgets, budgetErr := readBudgets(budgetReader)
	results, resultErr := readResults(resultReader)
	if _, err := fmt.Fprintf(out, "%-68s %12s %12s %10s\n", "benchmark", "ms/op", "budget ms/op", "headroom"); err != nil {
		return err
	}
	names := make([]string, 0, len(results)+len(budgets))
	for name := range results {
		names = append(names, name)
	}
	for name := range budgets {
		if _, ok := results[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	failures := []error{budgetErr, resultErr}
	if len(budgets) == 0 {
		failures = append(failures, errors.New("no budgets found"))
	}
	if len(results) == 0 {
		failures = append(failures, errors.New("no benchmark results found"))
	}
	for _, name := range names {
		figure, measured := results[name]
		budget, budgeted := budgets[name]
		switch {
		case !budgeted:
			failures = append(failures, fmt.Errorf("%s: %.3f ms/op has no budget", name, figure/1e6))
		case !measured:
			failures = append(failures, fmt.Errorf("%s: budget %.3f ms/op has no benchmark", name, budget/1e6))
		case figure > budget:
			failures = append(failures, fmt.Errorf("%s: %.3f ms/op exceeds budget %.3f ms/op", name, figure/1e6, budget/1e6))
		}
		figureText, budgetText, headroom := "missing", "missing", "-"
		if measured {
			figureText = fmt.Sprintf("%.3f", figure/1e6)
		}
		if budgeted {
			budgetText = fmt.Sprintf("%.3f", budget/1e6)
		}
		if measured && budgeted {
			headroom = fmt.Sprintf("%.0f%%", 100*(budget/figure-1))
		}
		if _, err := fmt.Fprintf(out, "%-68s %12s %12s %10s\n", name, figureText, budgetText, headroom); err != nil {
			return err
		}
	}
	return errors.Join(failures...)
}

func readBudgets(reader io.Reader) (map[string]float64, error) {
	budgets := map[string]float64{}
	scanner := bufio.NewScanner(reader)
	for line := 1; scanner.Scan(); line++ {
		text, _, _ := strings.Cut(scanner.Text(), "#")
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) != 2 || !strings.HasPrefix(fields[0], "BenchmarkLargeLibraryStore/") {
			return nil, fmt.Errorf("budget line %d: expected benchmark and duration", line)
		}
		budget, err := time.ParseDuration(fields[1])
		if err != nil || budget <= 0 {
			return nil, fmt.Errorf("budget line %d: invalid positive duration %q", line, fields[1])
		}
		if _, exists := budgets[fields[0]]; exists {
			return nil, fmt.Errorf("budget line %d: duplicate %s", line, fields[0])
		}
		budgets[fields[0]] = float64(budget)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return budgets, nil
}

func readResults(reader io.Reader) (map[string]float64, error) {
	results := map[string]float64{}
	scanner := bufio.NewScanner(reader)
	for line := 1; scanner.Scan(); line++ {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "Benchmark") {
			continue
		}
		name := fields[0]
		// Go appends the processor count, which is not part of a budget's identity.
		if at := strings.LastIndexByte(name, '-'); at >= 0 {
			if processors, err := strconv.Atoi(name[at+1:]); err == nil && processors > 0 {
				name = name[:at]
			}
		}
		if len(fields) != 4 {
			return nil, fmt.Errorf("result line %d: expected benchmark, iterations, figure and unit", line)
		}
		iterations, err := strconv.Atoi(fields[1])
		if err != nil || iterations < 1 {
			return nil, fmt.Errorf("result line %d: invalid iteration count", line)
		}
		figure, err := strconv.ParseFloat(fields[2], 64)
		if err != nil || figure <= 0 || math.IsNaN(figure) || math.IsInf(figure, 0) {
			return nil, fmt.Errorf("result line %d: invalid positive figure", line)
		}
		unit := strings.TrimSuffix(fields[3], "/op")
		duration, err := time.ParseDuration("1" + unit)
		if err != nil || unit == fields[3] || duration <= 0 {
			return nil, fmt.Errorf("result line %d: unsupported unit %q", line, fields[3])
		}
		figure *= float64(duration)
		if math.IsInf(figure, 0) {
			return nil, fmt.Errorf("result line %d: figure overflows", line)
		}
		// Repeated samples are compared by their slowest figure, never their mean.
		results[name] = max(results[name], figure)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return results, nil
}
