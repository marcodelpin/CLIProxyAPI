package codex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// PreviousCredentials are the saved credentials that a new Codex credential
// replaces: the same account and email under an earlier file name, because the
// plan type in the name changed or because the name predates the account hash.
type PreviousCredentials []*coreauth.Auth

// FindPreviousCredentials returns the credentials that target replaces, the most
// recently updated first. target must be named by CredentialFileName with an
// account hash; any other record returns nil. A candidate must sit at the top of
// the store and carry exactly the name CredentialFileName gives its own email and
// plan type, so a file the user renamed or moved into a subdirectory is never
// matched. The migration is best effort: a store that cannot be listed is skipped
// with a warning, and the caller saves the new credential as before.
func FindPreviousCredentials(ctx context.Context, store coreauth.Store, target *coreauth.Auth) (PreviousCredentials, error) {
	if store == nil || target == nil || !isCodexRecord(target) {
		return nil, nil
	}
	email, accountID, planType := credentialIdentity(target)
	targetName := credentialBaseName(target)
	accountHash := accountHashSegment(targetName, email, planType)
	if email == "" || accountID == "" || accountHash == "" {
		return nil, nil
	}

	records, errList := store.List(ctx)
	if errList != nil {
		if !errors.Is(errList, os.ErrNotExist) {
			log.Warnf("codex: credentials of a previous plan not checked: %v", errList)
		}
		return nil, nil
	}
	var previous PreviousCredentials
	for _, candidate := range records {
		if isPreviousCredential(candidate, targetName, email, accountID, accountHash) {
			previous = append(previous, candidate)
		}
	}
	sort.SliceStable(previous, func(i, j int) bool {
		return previous[i].UpdatedAt.After(previous[j].UpdatedAt)
	})
	return previous, nil
}

// MergeInto copies the settings of the previous credentials into target, the most
// recently updated first. Values that target already holds are kept.
func (p PreviousCredentials) MergeInto(target *coreauth.Auth) {
	for _, previous := range p {
		coreauth.MergeExistingAuthMetadata(target, previous.Metadata)
	}
}

// Delete removes the previous credentials once the new one was saved at savedPath.
// It lists the store again and removes only a credential that is still stored under
// the same ID with the update time and content it had when it was found: one written
// in the meantime, one whose store reports no update time, or the file just saved,
// is kept. An empty savedPath keeps them all.
func (p PreviousCredentials) Delete(ctx context.Context, store coreauth.Store, savedPath string) error {
	if len(p) == 0 {
		return nil
	}
	if strings.TrimSpace(savedPath) == "" {
		return fmt.Errorf("new Codex credential was not persisted; previous credentials retained")
	}
	current, errList := store.List(ctx)
	if errList != nil {
		return fmt.Errorf("list Codex credentials before removing previous ones: %w", errList)
	}
	stored := make(map[string]*coreauth.Auth, len(current))
	for _, record := range current {
		if record != nil {
			stored[credentialID(record)] = record
		}
	}
	savedName := strings.ToLower(filepath.Base(savedPath))
	for _, previous := range p {
		now, found := stored[credentialID(previous)]
		if strings.ToLower(credentialBaseName(previous)) == savedName || !found || !unchanged(previous, now) {
			continue
		}
		if errDelete := store.Delete(ctx, credentialID(previous)); errDelete != nil {
			return fmt.Errorf("delete previous Codex credential %s: %w", credentialID(previous), errDelete)
		}
	}
	return nil
}

// unchanged reports whether now is still the credential found as previous: the
// same update time, which the store must report, and the same content.
func unchanged(previous, now *coreauth.Auth) bool {
	return !previous.UpdatedAt.IsZero() && now.UpdatedAt.Equal(previous.UpdatedAt) &&
		reflect.DeepEqual(previous.Metadata, now.Metadata)
}

// isPreviousCredential reports whether candidate holds the same Codex account and
// email as the target named targetName, under the name generated for its plan.
func isPreviousCredential(candidate *coreauth.Auth, targetName, email, accountID, accountHash string) bool {
	if candidate == nil || !isCodexRecord(candidate) || strings.ContainsAny(candidate.ID+candidate.FileName, `/\`) {
		return false
	}
	name := credentialBaseName(candidate)
	if name == "" || strings.EqualFold(name, targetName) {
		return false
	}
	candidateEmail := metadataField(candidate.Metadata, "email")
	if !strings.EqualFold(candidateEmail, email) ||
		!strings.EqualFold(metadataField(candidate.Metadata, "account_id"), accountID) {
		return false
	}
	planType := metadataField(candidate.Metadata, "plan_type")
	for _, hash := range []string{accountHash, ""} {
		if strings.EqualFold(name, CredentialFileName(email, planType, hash, true)) ||
			strings.EqualFold(name, unsanitizedCredentialFileName(candidateEmail, planType, hash)) {
			return true
		}
	}
	return false
}

// credentialIdentity reads email, account id and plan type from the record
// metadata, falling back to the Codex token storage the login paths attach.
func credentialIdentity(record *coreauth.Auth) (email, accountID, planType string) {
	email = metadataField(record.Metadata, "email")
	accountID = metadataField(record.Metadata, "account_id")
	planType = metadataField(record.Metadata, "plan_type")
	if storage, ok := record.Storage.(*CodexTokenStorage); ok && storage != nil {
		if email == "" {
			email = strings.TrimSpace(storage.Email)
		}
		if accountID == "" {
			accountID = strings.TrimSpace(storage.AccountID)
		}
		if planType == "" {
			planType = strings.TrimSpace(storage.PlanType)
		}
	}
	return email, accountID, planType
}

// accountHashSegment returns the account hash that CredentialFileName put into
// name for email and planType, or "" when name does not have that layout.
func accountHashSegment(name, email, planType string) string {
	const prefix = "codex-"
	const hashLength = 8
	if len(name) <= len(prefix)+hashLength || !strings.EqualFold(name[:len(prefix)], prefix) {
		return ""
	}
	accountHash := name[len(prefix) : len(prefix)+hashLength]
	if !strings.EqualFold(CredentialFileName(email, planType, accountHash, true), name) {
		return ""
	}
	return accountHash
}

func isCodexRecord(record *coreauth.Auth) bool {
	return strings.EqualFold(strings.TrimSpace(record.Provider), "codex")
}

func credentialID(record *coreauth.Auth) string {
	if id := strings.TrimSpace(record.ID); id != "" {
		return id
	}
	return strings.TrimSpace(record.FileName)
}

func credentialBaseName(record *coreauth.Auth) string {
	name := strings.TrimSpace(record.FileName)
	if name == "" {
		name = strings.TrimSpace(record.ID)
	}
	if name == "" {
		return ""
	}
	return filepath.Base(name)
}

func metadataField(metadata map[string]any, key string) string {
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}
