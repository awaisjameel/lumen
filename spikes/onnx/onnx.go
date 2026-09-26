// Package onnx opens ONNX Runtime sessions on the CPU or DirectML for the
// Phase 0 model spikes.
package onnx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
	"golang.org/x/sys/windows"
)

// LibDir holds onnxruntime.dll and DirectML.dll (ONNX Runtime 1.24.4, the
// last release with a DirectML build; onnxruntime_go v1.27.0 matches it).
var LibDir = filepath.Join(".cache", "lib")

var initOnce = sync.OnceValue(func() error {
	abs, _ := filepath.Abs(LibDir)
	// Windows ships an older DirectML.dll in System32 that would win the DLL
	// search; loading ours first by full path makes onnxruntime use it.
	if _, err := windows.LoadLibraryEx(filepath.Join(abs, "DirectML.dll"), 0, windows.LOAD_WITH_ALTERED_SEARCH_PATH); err != nil {
		return fmt.Errorf("load DirectML.dll: %w", err)
	}
	ort.SetSharedLibraryPath(filepath.Join(abs, "onnxruntime.dll"))
	level := ort.WithLogLevelError()
	if os.Getenv("ORT_VERBOSE") != "" {
		level = ort.WithLogLevelVerbose()
	}
	return ort.InitializeEnvironment(level)
})

// Version reports the loaded ONNX Runtime version.
func Version() string { initOnce(); return ort.GetVersion() }

// Session is one loaded model.
type Session struct {
	*ort.DynamicAdvancedSession
	In, Out  []ort.InputOutputInfo
	Provider string
}

// Open loads a model on "dml" (GPU via DirectML) or "cpu". threads <= 0
// lets ONNX Runtime choose.
func Open(path, provider string, threads int) (*Session, error) {
	if err := initOnce(); err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	opts, err := ort.NewSessionOptions()
	if err != nil {
		return nil, err
	}
	defer opts.Destroy()
	if threads > 0 {
		opts.SetIntraOpNumThreads(threads)
	}
	// Idle worker threads otherwise busy-wait after each run, starving other
	// sessions and libvips; a background indexer must not spin.
	if os.Getenv("ORT_SPIN") == "" {
		if err := opts.AddSessionConfigEntry("session.intra_op.allow_spinning", "0"); err != nil {
			return nil, err
		}
	}
	// ORT_OPT=basic|extended works around optimizer bugs (e.g. the fp16
	// SigLIP export trips SimplifiedLayerNormFusion in ORT 1.24).
	switch os.Getenv("ORT_OPT") {
	case "basic":
		opts.SetGraphOptimizationLevel(ort.GraphOptimizationLevelEnableBasic)
	case "extended":
		opts.SetGraphOptimizationLevel(ort.GraphOptimizationLevelEnableExtended)
	}
	switch provider {
	case "dml":
		// DirectML requires these two settings.
		if err := opts.SetMemPattern(false); err != nil {
			return nil, err
		}
		if err := opts.SetExecutionMode(ort.ExecutionModeSequential); err != nil {
			return nil, err
		}
		if err := opts.AppendExecutionProviderDirectML(0); err != nil {
			return nil, fmt.Errorf("directml: %w", err)
		}
	case "cpu":
	default:
		return nil, fmt.Errorf("unknown provider %q", provider)
	}
	in, out, err := ort.GetInputOutputInfoWithOptions(path, opts)
	if err != nil {
		return nil, err
	}
	names := func(l []ort.InputOutputInfo) []string {
		n := make([]string, len(l))
		for i, x := range l {
			n[i] = x.Name
		}
		return n
	}
	s, err := ort.NewDynamicAdvancedSession(path, names(in), names(out), opts)
	if err != nil {
		return nil, err
	}
	return &Session{DynamicAdvancedSession: s, In: in, Out: out, Provider: provider}, nil
}

// Describe lists inputs and outputs, for logs.
func (s *Session) Describe() string {
	var b strings.Builder
	for _, x := range s.In {
		fmt.Fprintf(&b, "in  %s %v %v\n", x.Name, x.DataType, x.Dimensions)
	}
	for _, x := range s.Out {
		fmt.Fprintf(&b, "out %s %v %v\n", x.Name, x.DataType, x.Dimensions)
	}
	return b.String()
}

// Run runs the model and returns every output; the caller destroys them.
func (s *Session) Run(inputs ...ort.Value) ([]ort.Value, error) {
	outs := make([]ort.Value, len(s.Out))
	if err := s.DynamicAdvancedSession.Run(inputs, outs); err != nil {
		return nil, err
	}
	return outs, nil
}

// Float32 copies a float32 output tensor's data and shape.
func Float32(v ort.Value) ([]float32, []int64, error) {
	t, ok := v.(*ort.Tensor[float32])
	if !ok {
		return nil, nil, fmt.Errorf("output is %T, not float32", v)
	}
	return append([]float32(nil), t.GetData()...), t.GetShape(), nil
}

// Destroy frees values, ignoring nils.
func Destroy(vs ...ort.Value) {
	for _, v := range vs {
		if v != nil {
			v.Destroy()
		}
	}
}
