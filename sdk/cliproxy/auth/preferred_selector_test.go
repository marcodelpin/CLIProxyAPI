package auth

import (
	"context"
	"testing"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/executor"
)

func preferredSelectorAuth(id, label, email string) *Auth {
	return &Auth{
		ID:       id,
		Provider: "codex",
		Label:    label,
		Status:   StatusActive,
		Metadata: map[string]any{"email": email},
	}
}

func TestPreferredAuthSelector(t *testing.T) {
	first := preferredSelectorAuth("a", "first@example.test", "first@example.test")
	preferred := preferredSelectorAuth("b", "preferred-label", "preferred@example.test")

	for _, tc := range []struct {
		name       string
		preference string
		auths      []*Auth
		prepare    func()
		want       string
	}{
		{name: "label match", preference: " PREFERRED-LABEL ", auths: []*Auth{first, preferred}, want: "b"},
		{name: "email match", preference: " PREFERRED@EXAMPLE.TEST ", auths: []*Auth{first, preferred}, want: "b"},
		{name: "absent preference", auths: []*Auth{first, preferred}, want: "a"},
		{name: "missing preferred candidate", preference: "preferred@example.test", auths: []*Auth{first}, want: "a"},
		{
			name: "disabled preferred falls back", preference: "preferred@example.test", auths: []*Auth{first, preferred}, want: "a",
			prepare: func() { preferred.Disabled = true; preferred.Status = StatusDisabled },
		},
		{
			name: "cooling preferred falls back", preference: "preferred@example.test", auths: []*Auth{first, preferred}, want: "a",
			prepare: func() {
				preferred.Quota = QuotaState{Exceeded: true, Reason: "credential_quota", NextRecoverAt: time.Now().Add(time.Hour)}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			preferred.Disabled = false
			preferred.Status = StatusActive
			preferred.Quota = QuotaState{}
			if tc.prepare != nil {
				tc.prepare()
			}
			selector := NewPreferredAuthSelector(&FillFirstSelector{})
			opts := cliproxyexecutor.Options{}
			if tc.preference != "" {
				opts.Metadata = map[string]any{cliproxyexecutor.PreferredAuthMetadataKey: tc.preference}
			}
			got, err := selector.Pick(context.Background(), "codex", "", opts, tc.auths)
			if err != nil {
				t.Fatal(err)
			}
			if got == nil || got.ID != tc.want {
				t.Fatalf("selected = %#v, want %s", got, tc.want)
			}
		})
	}
}

func TestPreferredAuthSelectorDoesNotCrossPriorityTier(t *testing.T) {
	high := preferredSelectorAuth("a", "high@example.test", "high@example.test")
	high.Attributes = map[string]string{"priority": "10"}
	lowPreferred := preferredSelectorAuth("b", "preferred@example.test", "preferred@example.test")
	selector := NewPreferredAuthSelector(&FillFirstSelector{})
	opts := cliproxyexecutor.Options{Metadata: map[string]any{
		cliproxyexecutor.PreferredAuthMetadataKey: "preferred@example.test",
	}}
	got, err := selector.Pick(context.Background(), "codex", "", opts, []*Auth{lowPreferred, high})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != high.ID {
		t.Fatalf("selected = %#v, want highest-priority auth %s", got, high.ID)
	}
}
