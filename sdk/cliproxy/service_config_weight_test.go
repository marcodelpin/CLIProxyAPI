package cliproxy

import (
	"context"
	"reflect"
	"testing"

	internalconfig "github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

func TestWeightedRoundRobinRoutingSelector(t *testing.T) {
	state := normalizedRoutingRuntimeState(&internalconfig.Config{
		Routing: internalconfig.RoutingConfig{Strategy: "wrr"},
	})
	if state.strategy != "weighted-round-robin" {
		t.Fatalf("strategy = %q, want weighted-round-robin", state.strategy)
	}
	selector := newRoutingSelector(state)
	// The configured strategy now sits BENEATH the preferred-account layer, so
	// the assertion unwraps instead of reading the outer type. Dropping the
	// unwrap would let a future rewiring swap the strategy out unnoticed.
	if _, ok := coreauth.UnwrapPreferredAuthSelector(selector).(*coreauth.WeightedRoundRobinSelector); !ok {
		t.Fatalf("configured selector type = %T, want *auth.WeightedRoundRobinSelector", coreauth.UnwrapPreferredAuthSelector(selector))
	}
}

// TestRoutingSelectorAlwaysWrapsPreferredAuth pins the layer the test above now
// unwraps: every strategy must carry the preferred-account selector, otherwise
// the X-CLIProxy-Preferred-Auth header is silently ignored for that strategy.
func TestRoutingSelectorAlwaysWrapsPreferredAuth(t *testing.T) {
	for _, tc := range []struct {
		strategy string
		want     coreauth.Selector
	}{
		{strategy: "wrr", want: &coreauth.WeightedRoundRobinSelector{}},
		{strategy: "fill-first", want: &coreauth.FillFirstSelector{}},
		{strategy: "round-robin", want: &coreauth.RoundRobinSelector{}},
	} {
		t.Run(tc.strategy, func(t *testing.T) {
			state := normalizedRoutingRuntimeState(&internalconfig.Config{
				Routing: internalconfig.RoutingConfig{Strategy: tc.strategy},
			})
			selector := newRoutingSelector(state)
			if _, ok := selector.(*coreauth.PreferredAuthSelector); !ok {
				t.Fatalf("outer selector type = %T, want *auth.PreferredAuthSelector", selector)
			}
			inner := coreauth.UnwrapPreferredAuthSelector(selector)
			if reflect.TypeOf(inner) != reflect.TypeOf(tc.want) {
				t.Fatalf("configured selector type = %T, want %T", inner, tc.want)
			}
		})
	}
}

func TestServiceRejectsInvalidCredentialWeightConfigCommit(t *testing.T) {
	originalCfg := &internalconfig.Config{}
	service := &Service{cfg: originalCfg}
	invalidWeight := internalconfig.MaxCredentialWeight + 1
	newCfg := &internalconfig.Config{
		VertexCompatAPIKey: []internalconfig.VertexCompatKey{{
			APIKey: "vertex-key",
			Weight: &invalidWeight,
		}},
	}

	if service.applyConfigUpdateWithAuthSynthesis(nil, newCfg, true) {
		t.Fatal("hot config application accepted an invalid credential weight")
	}
	if service.cfg != originalCfg {
		t.Fatal("invalid hot config replaced the active config")
	}
	if service.configSequence != 0 {
		t.Fatalf("config sequence = %d, want 0", service.configSequence)
	}
}

type trackingStoppableSelector struct {
	stopped bool
}

func (s *trackingStoppableSelector) Pick(ctx context.Context, provider, model string, opts cliproxyexecutor.Options, auths []*coreauth.Auth) (*coreauth.Auth, error) {
	return nil, nil
}

func (s *trackingStoppableSelector) Stop() {
	s.stopped = true
}

func TestApplyManagerConfigStopsReplacedServiceAffinitySelector(t *testing.T) {
	tracking := &trackingStoppableSelector{}
	service := &Service{
		coreManager: coreauth.NewManager(nil, tracking, nil),
	}

	newCfg := &internalconfig.Config{
		Routing: internalconfig.RoutingConfig{
			Strategy: "round-robin",
		},
	}
	commit := configCommit{cfg: newCfg, sequence: 1}
	if !service.applyManagerConfig(context.Background(), commit) {
		t.Fatal("applyManagerConfig failed")
	}

	if !tracking.stopped {
		t.Fatal("expected replaced selector to be stopped during routing config apply")
	}
}
