// Package ai runs Lumen's models with ONNX Runtime: SigLIP 2 for search and
// Things, YuNet and SFace for faces (OCR later). It knows nothing about the
// database; the index package feeds it pixels and text and stores what
// comes back.
//
// ONNX Runtime 1.24.4 (the last DirectML build) and DirectML ship beside the
// executable (deps/runtime in development). The image model runs on the GPU
// through DirectML when one is usable, and on the CPU otherwise; the small
// text and face models always run on the CPU.
package ai

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"

	ort "github.com/yalue/onnxruntime_go"

	"lumen/internal/core"
)

var (
	initMu  sync.Mutex
	initDir string
	errInit error // Init's result, returned again by later calls
)

// Init loads ONNX Runtime from dir. Later calls return the first result.
func Init(dir string) error {
	initMu.Lock()
	defer initMu.Unlock()
	if initDir != "" {
		return errInit
	}
	initDir = dir
	// Without DirectML only the CPU is available, which still works.
	if err := preloadDirectML(dir); err != nil {
		slog.Warn("DirectML unavailable; models will run on the CPU", "err", err)
	}
	ort.SetSharedLibraryPath(filepath.Join(dir, "onnxruntime.dll"))
	if err := ort.InitializeEnvironment(ort.WithLogLevelError()); err != nil {
		errInit = fmt.Errorf("start ONNX Runtime: %w", err)
		return errInit
	}
	slog.Info("ONNX Runtime ready", "version", ort.GetVersion())
	return nil
}

// Shutdown releases ONNX Runtime. Every session must be closed first.
func Shutdown() {
	initMu.Lock()
	defer initMu.Unlock()
	if initDir != "" && errInit == nil {
		if err := ort.DestroyEnvironment(); err != nil {
			slog.Warn("stop ONNX Runtime", "err", err)
		}
	}
}

// Device is where a model runs.
type Device = core.Device

// The devices.
const (
	GPU = core.DeviceGPU
	CPU = core.DeviceCPU
)

// session is one loaded model: one input, float32 outputs.
type session struct {
	s       *ort.DynamicAdvancedSession
	device  Device
	outputs []string
}

// openSession loads a model on the given device. threads limits CPU threads
// (0 = ONNX Runtime's choice). outputs names the outputs run returns; with
// none it returns the model's embedding (pooler_output when the model has
// one, else its first output).
func openSession(path string, device Device, threads int, outputs ...string) (*session, error) {
	opts, err := ort.NewSessionOptions()
	if err != nil {
		return nil, err
	}
	defer destroy(opts)
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
	if len(outputs) == 0 {
		// Prepared models have one output; a raw export also has the hidden
		// states, and only the pooled embedding is wanted.
		outputs = []string{out[0].Name}
		for _, o := range out {
			if o.Name == "pooler_output" {
				outputs[0] = o.Name
			}
		}
	}
	s, err := ort.NewDynamicAdvancedSession(path, []string{in[0].Name}, outputs, opts)
	if err != nil {
		return nil, fmt.Errorf("load model %s: %w", filepath.Base(path), err)
	}
	return &session{s: s, device: device, outputs: outputs}, nil
}

// errClosed means the model was freed (or failed to reload after a GPU error).
var errClosed = errors.New("model is not loaded")

// run feeds one input and returns each output's data (copied), in the order
// openSession named them. Safe for concurrent use.
func (s *session) run(in ort.Value) ([][]float32, error) {
	if s == nil || s.s == nil {
		return nil, errClosed
	}
	outs := make([]ort.Value, len(s.outputs)) // ONNX Runtime allocates them
	if err := s.s.Run([]ort.Value{in}, outs); err != nil {
		return nil, err
	}
	data := make([][]float32, len(outs))
	var err error
	for i, o := range outs {
		if t, ok := o.(*ort.Tensor[float32]); ok {
			data[i] = append([]float32(nil), t.GetData()...)
		} else if err == nil {
			err = fmt.Errorf("model output %s is not float32", s.outputs[i])
		}
		destroy(o)
	}
	return data, err
}

func (s *session) close() {
	if s != nil && s.s != nil {
		destroy(s.s)
		s.s = nil
	}
}

// destroy frees an ONNX Runtime object. Freeing can't usefully fail, and
// there is nothing to do if it does, so the error is dropped here, once.
func destroy(v interface{ Destroy() error }) { _ = v.Destroy() }
