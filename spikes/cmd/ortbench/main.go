// Micro-benchmark: bare ONNX Runtime run time for a model at several thread
// counts, to separate model cost from pipeline overhead.
package main

import (
	"flag"
	"fmt"
	"strings"
	"time"

	ort "github.com/yalue/onnxruntime_go"

	"lumen/spikes/onnx"
)

func main() {
	model := flag.String("m", "", "model path")
	shape := flag.String("shape", "1,3,112,112", "input shape")
	ep := flag.String("ep", "cpu", "provider")
	threads := flag.String("threads", "0,1,2,4,8", "intra-op thread counts (0 = default)")
	iters := flag.Int("i", 30, "iterations")
	flag.Parse()
	var dims []int64
	size := int64(1)
	for _, s := range strings.Split(*shape, ",") {
		var d int64
		fmt.Sscan(s, &d)
		dims = append(dims, d)
		size *= d
	}
	for _, ts := range strings.Split(*threads, ",") {
		var t int
		fmt.Sscan(ts, &t)
		s, err := onnx.Open(*model, *ep, t)
		if err != nil {
			panic(err)
		}
		in, _ := ort.NewTensor(ort.NewShape(dims...), make([]float32, size))
		outs, err := s.Run(in) // warm-up
		if err != nil {
			panic(err)
		}
		onnx.Destroy(outs...)
		start := time.Now()
		for range *iters {
			outs, _ := s.Run(in)
			onnx.Destroy(outs...)
		}
		fmt.Printf("%s threads=%d: %.1f ms/run\n", *ep, t, float64(time.Since(start).Microseconds())/1000/float64(*iters))
		in.Destroy()
		s.Destroy()
	}
}
