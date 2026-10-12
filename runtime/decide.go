package runtime

import (
	"context"
	"fmt"

	"github.com/samcharles93/ai-sdk/decide"
	"github.com/samcharles93/ai-sdk/provider/systemone"
)

// DecideProvider resolves a decision provider, refusing published metadata that
// excludes decisions. Unknown compatible models are discovered from the server.
func (r *Runtime) DecideProvider(ctx context.Context, ref string) (decide.Provider, string, error) {
	mref, err := r.ParseModelRef(ref)
	if err != nil {
		return nil, "", err
	}
	model, err := r.resolveModel(mref)
	if err != nil {
		return nil, "", err
	}
	supported, known := modelSupports(model, CapabilityDecide)
	if known && !supported {
		return nil, "", fmt.Errorf("%w: model %q does not declare decisions output", ErrCapabilityNotSupported, model.ID)
	}
	set, err := r.providerSetFor(ctx, mref.ProviderID, model)
	if err != nil {
		return nil, "", err
	}
	if set.Decide == nil {
		return nil, "", fmt.Errorf("%w: provider %q does not support decisions", ErrCapabilityNotSupported, mref.ProviderID)
	}
	if !known {
		if p, ok := set.Decide.(*systemone.Provider); ok {
			models, err := p.Models(ctx)
			if err != nil {
				return nil, "", err
			}
			for _, m := range models {
				if m.ID == model.ID {
					model.OutputModalities = m.Architecture.OutputModalities
					break
				}
			}
			if supported, known := modelSupports(model, CapabilityDecide); known && !supported {
				return nil, "", fmt.Errorf("%w: model %q does not declare decisions output", ErrCapabilityNotSupported, model.ID)
			}
		}
	}
	return set.Decide, model.ID, nil
}

// Decide answers named questions using the resolved model.
func (r *Runtime) Decide(ctx context.Context, ref string, req decide.Request) (decide.Response, error) {
	p, model, err := r.DecideProvider(ctx, ref)
	if err != nil {
		return decide.Response{}, err
	}
	req.Model = model
	return decide.NewClient(p).Decide(ctx, req)
}
