package codex

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

const (
	previousTestEmail   = "user@example.com"
	previousTestAccount = "account-a"
	previousTestHash    = "abc12345"
)

type previousCredentialStore struct {
	records []*coreauth.Auth
	listErr error
	deleted []string
}

func (s *previousCredentialStore) List(context.Context) ([]*coreauth.Auth, error) {
	return s.records, s.listErr
}

func (*previousCredentialStore) Save(context.Context, *coreauth.Auth) (string, error) {
	panic("unexpected Save call")
}

func (s *previousCredentialStore) Delete(_ context.Context, id string) error {
	s.deleted = append(s.deleted, id)
	return nil
}

type panicListStore struct{ previousCredentialStore }

func (*panicListStore) List(context.Context) ([]*coreauth.Auth, error) {
	panic("unexpected List call")
}

func previousTestTarget() *coreauth.Auth {
	name := CredentialFileName(previousTestEmail, "plus", previousTestHash, true)
	return &coreauth.Auth{
		ID:       name,
		FileName: name,
		Provider: "codex",
		Metadata: map[string]any{"email": previousTestEmail, "account_id": previousTestAccount, "plan_type": "plus"},
	}
}

func previousTestCandidate(name, email, accountID, planType string) *coreauth.Auth {
	return &coreauth.Auth{
		ID:       name,
		FileName: name,
		Provider: "codex",
		Metadata: map[string]any{"type": "codex", "email": email, "account_id": accountID, "plan_type": planType},
	}
}

func TestFindPreviousCredentials(t *testing.T) {
	freeName := CredentialFileName(previousTestEmail, "free", previousTestHash, true)
	otherProvider := previousTestCandidate(freeName, previousTestEmail, previousTestAccount, "free")
	otherProvider.Provider = "claude"
	tests := []struct {
		name      string
		candidate *coreauth.Auth
		wantMatch bool
	}{
		{"same account and email under another plan", previousTestCandidate(freeName, previousTestEmail, previousTestAccount, "free"), true},
		{"same account and email without plan", previousTestCandidate(CredentialFileName(previousTestEmail, "", previousTestHash, true), previousTestEmail, previousTestAccount, ""), true},
		{"layout without the account hash", previousTestCandidate(CredentialFileName(previousTestEmail, "free", "", true), previousTestEmail, previousTestAccount, "free"), true},
		{"email differs only in case", previousTestCandidate(freeName, "User@Example.com", previousTestAccount, "free"), true},
		{"another account id in the file", previousTestCandidate(freeName, previousTestEmail, "account-b", "free"), false},
		{"another email in the file", previousTestCandidate(freeName, "other@example.com", previousTestAccount, "free"), false},
		{"another member of the same account", previousTestCandidate(CredentialFileName("other@example.com", "free", previousTestHash, true), "other@example.com", previousTestAccount, "free"), false},
		{"another account hash in the name", previousTestCandidate(CredentialFileName(previousTestEmail, "free", "def67890", true), previousTestEmail, previousTestAccount, "free"), false},
		{"name chosen by the user", previousTestCandidate("my-codex.json", previousTestEmail, previousTestAccount, "free"), false},
		{"backup renamed with a plan-like suffix", previousTestCandidate("codex-abc12345-user@example.com-free-backup.json", previousTestEmail, previousTestAccount, "free"), false},
		{"copy of the file under another name", previousTestCandidate("codex-abc12345-user@example.com-free (copy).json", previousTestEmail, previousTestAccount, "free"), false},
		{"name of another plan than the file records", previousTestCandidate(CredentialFileName(previousTestEmail, "team", previousTestHash, true), previousTestEmail, previousTestAccount, "free"), false},
		{"credential in a subdirectory", previousTestCandidate("team/"+freeName, previousTestEmail, previousTestAccount, "free"), false},
		{"the target itself", previousTestCandidate(CredentialFileName(previousTestEmail, "plus", previousTestHash, true), previousTestEmail, previousTestAccount, "plus"), false},
		{"candidate without account id", previousTestCandidate(freeName, previousTestEmail, "", "free"), false},
		{"another provider", otherProvider, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &previousCredentialStore{records: []*coreauth.Auth{tt.candidate}}
			got, err := FindPreviousCredentials(context.Background(), store, previousTestTarget())
			if err != nil {
				t.Fatalf("FindPreviousCredentials() error = %v", err)
			}
			if len(got) > 1 || (len(got) == 1) != tt.wantMatch {
				t.Fatalf("FindPreviousCredentials() returned %d credentials, want match = %v", len(got), tt.wantMatch)
			}
		})
	}
}

// TestFindPreviousCredentialsRecognizesNamesBeforeTheSanitizer covers a credential
// saved before CredentialFileName replaced characters such as "?" in the email: its
// name differs from the one generated now, for the same account and email.
func TestFindPreviousCredentialsRecognizesNamesBeforeTheSanitizer(t *testing.T) {
	const email = "a?b@example.com"
	target := previousTestTarget()
	target.Metadata["email"] = email
	target.ID = CredentialFileName(email, "plus", previousTestHash, true)
	target.FileName = target.ID
	for _, name := range []string{
		"codex-abc12345-a?b@example.com-plus.json",
		"codex-abc12345-a?b@example.com-free.json",
		"codex-a?b@example.com-free.json",
	} {
		t.Run(name, func(t *testing.T) {
			planType := "free"
			if strings.HasSuffix(name, "-plus.json") {
				planType = "plus"
			}
			candidate := previousTestCandidate(name, email, previousTestAccount, planType)
			got, err := FindPreviousCredentials(context.Background(), &previousCredentialStore{records: []*coreauth.Auth{candidate}}, target)
			if err != nil || len(got) != 1 {
				t.Fatalf("FindPreviousCredentials() = %d credentials, %v, want 1, nil", len(got), err)
			}
		})
	}
}

// TestFindPreviousCredentialsSkipsTargetsItCannotName checks that a target without
// the hashed layout never lists the store, so no credential can be matched to it.
func TestFindPreviousCredentialsSkipsTargetsItCannotName(t *testing.T) {
	withoutAccount := previousTestTarget()
	delete(withoutAccount.Metadata, "account_id")
	customName := previousTestTarget()
	customName.ID, customName.FileName = "my-codex.json", "my-codex.json"
	otherProvider := previousTestTarget()
	otherProvider.Provider = "claude"
	otherEmailName := previousTestTarget()
	otherEmailName.ID = CredentialFileName("other@example.com", "plus", previousTestHash, true)
	otherEmailName.FileName = otherEmailName.ID
	for name, target := range map[string]*coreauth.Auth{
		"no account id":           withoutAccount,
		"custom name":             customName,
		"other provider":          otherProvider,
		"name of another address": otherEmailName,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := FindPreviousCredentials(context.Background(), &panicListStore{}, target)
			if err != nil || got != nil {
				t.Fatalf("FindPreviousCredentials() = %v, %v, want nil, nil", got, err)
			}
		})
	}
}

// TestFindPreviousCredentialsReadsTheTokenStorage covers the login records, which
// carry the account id in the token storage only.
func TestFindPreviousCredentialsReadsTheTokenStorage(t *testing.T) {
	target := previousTestTarget()
	delete(target.Metadata, "account_id")
	target.Storage = &CodexTokenStorage{AccountID: previousTestAccount}
	candidate := previousTestCandidate(CredentialFileName(previousTestEmail, "free", previousTestHash, true), previousTestEmail, previousTestAccount, "free")
	got, err := FindPreviousCredentials(context.Background(), &previousCredentialStore{records: []*coreauth.Auth{candidate}}, target)
	if err != nil || len(got) != 1 {
		t.Fatalf("FindPreviousCredentials() = %d credentials, %v, want 1, nil", len(got), err)
	}
}

func TestFindPreviousCredentialsOrdersNewestFirst(t *testing.T) {
	older := previousTestCandidate(CredentialFileName(previousTestEmail, "free", previousTestHash, true), previousTestEmail, previousTestAccount, "free")
	older.UpdatedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := previousTestCandidate(CredentialFileName(previousTestEmail, "team", previousTestHash, true), previousTestEmail, previousTestAccount, "team")
	newer.UpdatedAt = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	got, err := FindPreviousCredentials(context.Background(), &previousCredentialStore{records: []*coreauth.Auth{older, newer}}, previousTestTarget())
	if err != nil {
		t.Fatalf("FindPreviousCredentials() error = %v", err)
	}
	if len(got) != 2 || got[0] != newer || got[1] != older {
		t.Fatalf("FindPreviousCredentials() order = %v, want newest first", got)
	}
}

// TestFindPreviousCredentialsSkipsAStoreItCannotList checks that the migration is
// best effort: a store that cannot be listed must not fail the save that follows.
func TestFindPreviousCredentialsSkipsAStoreItCannotList(t *testing.T) {
	for name, listErr := range map[string]error{
		"missing":      os.ErrNotExist,
		"unconfigured": errors.New("auth filestore: directory not configured"),
	} {
		t.Run(name, func(t *testing.T) {
			got, err := FindPreviousCredentials(context.Background(), &previousCredentialStore{listErr: listErr}, previousTestTarget())
			if err != nil || got != nil {
				t.Fatalf("FindPreviousCredentials() = %v, %v, want nil, nil", got, err)
			}
		})
	}
}

func TestPreviousCredentialsMergeIntoPrefersNewest(t *testing.T) {
	newer := &coreauth.Auth{Metadata: map[string]any{"proxy_url": "http://newer", "prefix": "team"}}
	older := &coreauth.Auth{Metadata: map[string]any{"proxy_url": "http://older", "weight": float64(3)}}
	target := &coreauth.Auth{Metadata: map[string]any{"prefix": "mine"}}
	PreviousCredentials{newer, older}.MergeInto(target)
	want := map[string]any{"proxy_url": "http://newer", "prefix": "mine", "weight": float64(3)}
	if !reflect.DeepEqual(target.Metadata, want) {
		t.Fatalf("merged metadata = %v, want %v", target.Metadata, want)
	}
}

func TestPreviousCredentialsDelete(t *testing.T) {
	const savedPath = "/auths/codex-abc12345-user@example.com-plus.json"
	found := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	record := func(name string, updatedAt time.Time) *coreauth.Auth {
		return &coreauth.Auth{ID: name, FileName: name, UpdatedAt: updatedAt}
	}
	free := record("codex-abc12345-user@example.com-free.json", found)
	team := record("codex-user@example.com-team.json", found)
	plus := record("codex-abc12345-user@example.com-plus.json", found)
	undated := record(free.ID, time.Time{})
	replaced := record(free.ID, found)
	replaced.Metadata = map[string]any{"access_token": "written-by-another-login"}
	nestedCopy := record("backup/"+free.ID, found)

	tests := []struct {
		name      string
		previous  PreviousCredentials
		stored    []*coreauth.Auth
		listErr   error
		savedPath string
		wantErr   bool
		want      []string
	}{
		{name: "removes what is still as it was found", previous: PreviousCredentials{free, team}, stored: []*coreauth.Auth{free, team, plus}, savedPath: savedPath, want: []string{free.ID, team.ID}},
		{name: "keeps everything without a saved path", previous: PreviousCredentials{free}, stored: []*coreauth.Auth{free}, wantErr: true},
		{name: "keeps a credential written since it was found", previous: PreviousCredentials{free, team}, stored: []*coreauth.Auth{record(free.ID, found.Add(time.Minute)), team}, savedPath: savedPath, want: []string{team.ID}},
		{name: "skips a credential that is already gone", previous: PreviousCredentials{free, team}, stored: []*coreauth.Auth{team}, savedPath: savedPath, want: []string{team.ID}},
		{name: "keeps the file that was just saved", previous: PreviousCredentials{free, plus}, stored: []*coreauth.Auth{free, plus}, savedPath: savedPath, want: []string{free.ID}},
		{name: "keeps everything when the store cannot be listed", previous: PreviousCredentials{free}, listErr: errors.New("permission denied"), savedPath: savedPath, wantErr: true},
		{name: "keeps a credential whose store reports no update time", previous: PreviousCredentials{undated}, stored: []*coreauth.Auth{undated}, savedPath: savedPath},
		{name: "keeps a replacement written with the same update time", previous: PreviousCredentials{free}, stored: []*coreauth.Auth{replaced}, savedPath: savedPath},
		{name: "a nested copy does not stand for the credential", previous: PreviousCredentials{free}, stored: []*coreauth.Auth{record(free.ID, found.Add(time.Minute)), nestedCopy}, savedPath: savedPath},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &previousCredentialStore{records: tt.stored, listErr: tt.listErr}
			err := tt.previous.Delete(context.Background(), store, tt.savedPath)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Delete() error = %v, want an error = %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(store.deleted, tt.want) {
				t.Fatalf("deleted = %v, want %v", store.deleted, tt.want)
			}
		})
	}
}
