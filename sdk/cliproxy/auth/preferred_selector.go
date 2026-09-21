package auth

import (
	"context"
	"strings"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

// PreferredAuthSelector tries a requested account before the configured selector.
// It belongs beneath session affinity so established bindings remain authoritative.
type PreferredAuthSelector struct {
	fallback Selector
}

// NewPreferredAuthSelector wraps a selector with best-effort account preference.
func NewPreferredAuthSelector(fallback Selector) *PreferredAuthSelector {
	if fallback == nil {
		fallback = &RoundRobinSelector{}
	}
	return &PreferredAuthSelector{fallback: fallback}
}

// Pick selects an available preferred account within the highest priority tier,
// or delegates unchanged to the configured selector when no match is available.
func (s *PreferredAuthSelector) Pick(ctx context.Context, provider, model string, opts cliproxyexecutor.Options, auths []*Auth) (*Auth, error) {
	if preferred := preferredAuthFromMetadata(opts.Metadata); preferred != "" {
		candidates := auths
		if _, weighted := unwrapPreferredAuthSelector(s.fallback).(*WeightedRoundRobinSelector); weighted {
			candidates = positiveWeightAuths(candidates)
		}
		available, errAvailable := getSelectorAvailableAuths(ctx, candidates, provider, model, time.Now())
		if errAvailable == nil {
			for _, candidate := range available {
				email, _ := candidate.Metadata["email"].(string)
				if strings.EqualFold(strings.TrimSpace(candidate.Label), preferred) || strings.EqualFold(strings.TrimSpace(email), preferred) {
					return candidate, nil
				}
			}
		}
	}
	return s.fallback.Pick(ctx, provider, model, opts, auths)
}

func preferredAuthFromMetadata(metadata map[string]any) string {
	preferred, _ := metadata[cliproxyexecutor.PreferredAuthMetadataKey].(string)
	return strings.TrimSpace(preferred)
}

// unwrapPreferredAuthSelector peels off any PreferredAuthSelector wrappers so
// callers keep type-switching on the configured selector they actually set up.
// Returns selector unchanged when it is not a PreferredAuthSelector.
func unwrapPreferredAuthSelector(selector Selector) Selector {
	for {
		preferred, ok := selector.(*PreferredAuthSelector)
		if !ok {
			return selector
		}
		selector = preferred.fallback
	}
}

// UnwrapPreferredAuthSelector is the exported form of the helper above, for
// tests and callers outside this package that assert on the configured selector.
func UnwrapPreferredAuthSelector(selector Selector) Selector {
	return unwrapPreferredAuthSelector(selector)
}
