package registry

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestEmbeddedCodexCatalogsIncludeAstraAndSol(t *testing.T) {
	var embedded staticModelsJSON
	if err := json.Unmarshal(embeddedModelsJSON, &embedded); err != nil {
		t.Fatalf("decode embedded models: %v", err)
	}
	for _, tc := range []struct {
		name     string
		embedded []*ModelInfo
		get      func() []*ModelInfo
	}{
		{name: "team", embedded: embedded.CodexTeam, get: GetCodexTeamModels},
		{name: "plus", embedded: embedded.CodexPlus, get: GetCodexPlusModels},
		{name: "pro", embedded: embedded.CodexPro, get: GetCodexProModels},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, models := range [][]*ModelInfo{tc.embedded, tc.get()} {
				for _, id := range []string{"gpt-6-astra", "gpt-5.6-sol"} {
					var found *ModelInfo
					for _, model := range models {
						if model != nil && model.ID == id {
							if found != nil {
								t.Fatalf("duplicate model %q", id)
							}
							found = model
						}
					}
					if found == nil {
						t.Fatalf("missing model %q", id)
					}
					if found.Object != "model" || found.OwnedBy != "openai" || found.Type != "openai" {
						t.Errorf("model %q has invalid Codex metadata: object=%q owner=%q type=%q", id, found.Object, found.OwnedBy, found.Type)
					}
					if found.DisplayName == "" || found.Version == "" || found.ContextLength <= 0 || found.MaxCompletionTokens <= 0 {
						t.Errorf("model %q has incomplete local metadata", id)
					}
					wantLevels := []string{"low", "medium", "high", "xhigh", "max"}
					if id == "gpt-6-astra" {
						wantLevels = append(wantLevels, "ultra")
					}
					if found.Thinking == nil || !slices.Equal(found.Thinking.Levels, wantLevels) {
						t.Errorf("model %q thinking = %+v, want levels %v", id, found.Thinking, wantLevels)
					}
				}
			}
		})
	}
}

func TestEmbeddedCodexClientCatalogIncludesAstraUltraAndSol(t *testing.T) {
	var catalog struct {
		Models []struct {
			Slug                     string `json:"slug"`
			SupportedReasoningLevels []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if err := json.Unmarshal(embeddedCodexClientModelsJSON, &catalog); err != nil {
		t.Fatalf("decode embedded Codex client models: %v", err)
	}
	for _, id := range []string{"gpt-6-astra", "gpt-5.6-sol"} {
		found := false
		for _, model := range catalog.Models {
			if model.Slug != id {
				continue
			}
			found = true
			var levels []string
			for _, level := range model.SupportedReasoningLevels {
				levels = append(levels, level.Effort)
			}
			if !slices.Contains(levels, "ultra") {
				t.Errorf("model %q levels = %v, want ultra", id, levels)
			}
		}
		if !found {
			t.Errorf("missing Codex client model %q", id)
		}
	}
}
