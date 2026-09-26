// Package ai runs Lumen's models with ONNX Runtime: SigLIP 2 for search and
// tags now, faces and OCR later. It knows nothing about the database; the
// index package feeds it pixels and text and stores what comes back.
//
// ONNX Runtime 1.24.4 (the last DirectML build) and DirectML ship beside the
// executable (deps/runtime in development). Every model runs on the GPU
// through DirectML when one is usable, and on the CPU otherwise.
package ai

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

var (
	initMu  sync.Mutex
	initDir string
	initErr error
)

// Init loads ONNX Runtime from dir. Later calls return the first result.
func Init(dir string) error {
	initMu.Lock()
	defer initMu.Unlock()
	if initDir != "" {
		return initErr
	}
	initDir = dir
	// Without DirectML only the CPU is available, which still works.
	if err := preloadDirectML(dir); err != nil {
		slog.Warn("DirectML unavailable; models will run on the CPU", "err", err)
	}
	ort.SetSharedLibraryPath(filepath.Join(dir, "onnxruntime.dll"))
	if err := ort.InitializeEnvironment(ort.WithLogLevelError()); err != nil {
		initErr = fmt.Errorf("start ONNX Runtime: %w", err)
		return initErr
	}
	slog.Info("ONNX Runtime ready", "version", ort.GetVersion())
	return nil
}

// Shutdown releases ONNX Runtime. Every session must be closed first.
func Shutdown() {
	initMu.Lock()
	defer initMu.Unlock()
	if initDir != "" && initErr == nil {
		ort.DestroyEnvironment()
	}
}

// Device is where a model runs.
type Device string

const (
	GPU Device = "gpu" // DirectML: any DirectX 12 GPU
	CPU Device = "cpu"
)

// session is one loaded model: one input, one (pooled) output.
type session struct {
	s      *ort.DynamicAdvancedSession
	device Device
}

// openSession loads a model on the given device. threads limits CPU threads
// (0 = ONNX Runtime's choice).
func openSession(path string, device Device, threads int) (*session, error) {
	opts, err := ort.NewSessionOptions()
	if err != nil {
		return nil, err
	}
	defer opts.Destroy()
	for _, kv := range [][2]string{
		// Idle worker threads otherwise busy-wait after every run, starving
		// libvips and other sessions (10 to 25 times slower in Phase 0).
		{"session.intra_op.allow_spinning", "0"},
		{"session.inter_op.allow_spinning", "0"},
	} {
		if err := opts.AddSessionConfigEntry(kv[0], kv[1]); err != nil {
			return nil, err
		}
	}
	// The full optimisation level mis-fuses the fp16 layer norms in these
	// exports (ONNX Runtime 1.24); extended keeps every useful fusion.
	if err := opts.SetGraphOptimizationLevel(ort.GraphOptimizationLevelEnableExtended); err != nil {
		return nil, err
	}
	if threads > 0 {
		if err := opts.SetIntraOpNumThreads(threads); err != nil {
			return nil, err
		}
	}
	if device == GPU {
		// DirectML requires both of these.
		if err := opts.SetMemPattern(false); err != nil {
			return nil, err
		}
		if err := opts.SetExecutionMode(ort.ExecutionModeSequential); err != nil {
			return nil, err
		}
		if err := opts.AppendExecutionProviderDirectML(0); err != nil {
			return nil, fmt.Errorf("DirectML: %w", err)
		}
	}
	in, out, err := ort.GetInputOutputInfoWithOptions(path, opts)
	if err != nil {
		return nil, fmt.Errorf("read model %s: %w", filepath.Base(path), err)
	}
	if len(in) != 1 || len(out) == 0 {
		return nil, fmt.Errorf("model %s: want one input and an output, got %d and %d", filepath.Base(path), len(in), len(out))
	}
	// Prepared models have one output; a raw export also has the hidden
	// states, and only the pooled embedding is wanted.
	output := out[0].Name
	for _, o := range out {
		if o.Name == "pooler_output" {
			output = o.Name
		}
	}
	s, err := ort.NewDynamicAdvancedSession(path, []string{in[0].Name}, []string{output}, opts)
	if err != nil {
		return nil, fmt.Errorf("load model %s: %w", filepath.Base(path), err)
	}
	return &session{s: s, device: device}, nil
}

// errClosed means the model was freed (or failed to reload after a GPU error).
var errClosed = errors.New("model is not loaded")

// run feeds one input and returns the float32 output's data (copied) and shape.
func (s *session) run(in ort.Value) ([]float32, ort.Shape, error) {
	if s == nil || s.s == nil {
		return nil, nil, errClosed
	}
	outs := []ort.Value{nil} // ONNX Runtime allocates the output
	if err := s.s.Run([]ort.Value{in}, outs); err != nil {
		return nil, nil, err
	}
	defer outs[0].Destroy()
	t, ok := outs[0].(*ort.Tensor[float32])
	if !ok {
		return nil, nil, errors.New("model output is not float32")
	}
	return append([]float32(nil), t.GetData()...), t.GetShape(), nil
}

func (s *session) close() {
	if s != nil && s.s != nil {
		s.s.Destroy()
		s.s = nil
	}
}
