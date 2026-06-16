package limiter

import "time"

// Backend identifies a limiter storage implementation (in-memory only in the core factory).
type Backend string

const (
	BackendInMemory Backend = "inmemory"
)

// Config holds resolved construction parameters passed to backend builders.
type Config struct {
	Limit    int
	Window   time.Duration
	Rate     float64
	Capacity int
	Clock    Clock
}

// Spec describes how to validate and build a limiter for an algorithm on a backend.
type Spec struct {
	Validate func(Config) error
	Build    func(Config) (Limiter, error)
}

var registries map[Backend]map[Algorithm]Spec

func register(backend Backend, algo Algorithm, spec Spec) {
	if registries == nil {
		registries = make(map[Backend]map[Algorithm]Spec)
	}
	if registries[backend] == nil {
		registries[backend] = make(map[Algorithm]Spec)
	}
	registries[backend][algo] = spec
}

func newLimiter(backend Backend, algo Algorithm, opts ...Option) (Limiter, error) {
	registry, ok := registries[backend]
	if !ok {
		return nil, ErrUnknownBackend
	}
	spec, ok := registry[algo]
	if !ok {
		return nil, ErrUnknownAlgorithm
	}

	cfg, err := applyOptions(opts)
	if err != nil {
		return nil, err
	}
	if err := spec.Validate(cfg); err != nil {
		return nil, err
	}
	return spec.Build(cfg)
}

// RegisterInMemory registers an in-memory algorithm builder.
// Called from backend packages (e.g. inmemory) via init.
func RegisterInMemory(algo Algorithm, spec Spec) {
	register(BackendInMemory, algo, spec)
}

// NewInMemory creates an in-memory limiter for the given algorithm.
// Import the inmemory package to register available algorithms:
//
//	import _ "github.com/swasthikshetty10/go-ratelimiter/limiter/inmemory"
func NewInMemory(algo Algorithm, opts ...Option) (Limiter, error) {
	return newLimiter(BackendInMemory, algo, opts...)
}
