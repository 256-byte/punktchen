package layer

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
)

// ErrUnknownLayer is returned when a request names a layer that is not
// registered (the API answers 400).
var ErrUnknownLayer = errors.New("unknown layer")

// Registry holds the providers. It is built once at startup and read-only
// afterwards, so it is safe for concurrent use.
type Registry struct {
	byID  map[string]Provider
	order []string // registration order; also the order of composed layers
}

// NewRegistry registers providers in the given order. IDs must be non-empty
// and unique.
func NewRegistry(providers ...Provider) (*Registry, error) {
	r := &Registry{byID: make(map[string]Provider, len(providers))}
	for _, p := range providers {
		id := p.Info().ID
		if id == "" {
			return nil, errors.New("layer: provider with empty id")
		}
		if _, dup := r.byID[id]; dup {
			return nil, fmt.Errorf("layer: duplicate provider id %q", id)
		}
		r.byID[id] = p
		r.order = append(r.order, id)
	}
	return r, nil
}

// Layers describes all registered layers in registration order.
func (r *Registry) Layers() []Info {
	out := make([]Info, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.byID[id].Info())
	}
	return out
}

// Get returns the provider for id.
func (r *Registry) Get(id string) (Provider, bool) {
	p, ok := r.byID[id]
	return p, ok
}

// Resolve turns requested layer ids into providers. Duplicates are removed and
// the result is in registration order, so the same set of layers always
// composes (and later caches) the same way. Unknown ids → ErrUnknownLayer
// naming all of them; no ids → an error too, since defaults are the API's
// choice.
func (r *Registry) Resolve(ids []string) ([]Provider, error) {
	if len(ids) == 0 {
		return nil, errors.New("layer: no layers requested")
	}
	var unknown []string
	for _, id := range ids {
		if _, ok := r.byID[id]; !ok && !slices.Contains(unknown, id) {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrUnknownLayer, strings.Join(unknown, ", "))
	}
	var out []Provider
	for _, id := range r.order {
		if slices.Contains(ids, id) {
			out = append(out, r.byID[id])
		}
	}
	return out, nil
}

// LayerResponse is one layer's part of a composed response.
type LayerResponse struct {
	Info    Info
	Version uint64
	Health  Health // Status is StatusDown when Err is set
	Payload any    // nil when Err is set
	Err     error  // provider error (or panic); the other layers are unaffected
}

// Composite is a composed multi-layer response.
type Composite struct {
	Layers []LayerResponse // in registration order
	// Attribution of all layers that returned data, without duplicates, in
	// layer order.
	Attribution []string
}

// Nearby asks every requested layer concurrently and composes the results.
// A failing layer is reported as down in its LayerResponse and does not fail
// the others. Nearby itself fails only for an invalid query, unknown layers
// or a cancelled context.
func (r *Registry) Nearby(ctx context.Context, q NearbyQuery, ids []string) (*Composite, error) {
	if err := q.Validate(); err != nil {
		return nil, err
	}
	providers, err := r.Resolve(ids)
	if err != nil {
		return nil, err
	}

	out := &Composite{Layers: make([]LayerResponse, len(providers))}
	var wg sync.WaitGroup
	for i, p := range providers {
		wg.Go(func() { out.Layers[i] = ask(ctx, p, q) })
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	for _, l := range out.Layers {
		if l.Err != nil {
			continue
		}
		for _, a := range l.Info.Attribution {
			if !slices.Contains(out.Attribution, a) {
				out.Attribution = append(out.Attribution, a)
			}
		}
	}
	return out, nil
}

// ask calls one provider. A panic becomes an error so one broken layer cannot
// take the process down (handler middleware does not see other goroutines).
func ask(ctx context.Context, p Provider, q NearbyQuery) (resp LayerResponse) {
	resp.Info = p.Info()
	defer func() {
		if v := recover(); v != nil {
			resp.Err = fmt.Errorf("layer %s: panic: %v", resp.Info.ID, v)
		}
		if resp.Err != nil {
			resp.Payload = nil
			resp.Health.Status = StatusDown
			resp.Health.LastError = resp.Err.Error()
		}
	}()

	res, err := p.Nearby(ctx, q)
	resp.Health = p.Health()
	if err != nil {
		resp.Err = fmt.Errorf("layer %s: %w", resp.Info.ID, err)
		return resp
	}
	resp.Version = res.Version
	resp.Payload = res.Payload
	return resp
}
