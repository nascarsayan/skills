package skillman

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLoadCatalogUsesRecursiveUpstreamBeforeOwnedSkills(t *testing.T) {
	root := t.TempDir()
	ghq := filepath.Join(root, "ghq")
	personal := filepath.Join(ghq, "github.com", "person", "skills")
	company := filepath.Join(ghq, "github.com", "company", "skills")
	official := filepath.Join(ghq, "github.com", "official", "skills")

	initTestRepo(t, personal, "git@github.com:person/skills.git")
	mustWriteSkill(t, filepath.Join(personal, "skills", "personal"), "personal", "personal skill")
	mustWriteJSON(t, filepath.Join(personal, "upstream-sources.json"), UpstreamManifest{Sources: []Source{
		{Repository: "company/skills", URL: "https://github.com/company/skills", Ref: "main", Skills: []ManifestSkill{{Name: "company-only", Path: "skills/company-only/SKILL.md"}}},
	}})

	mustWriteSkill(t, filepath.Join(company, "skills", "duplicate"), "duplicate", "company copy")
	mustWriteSkill(t, filepath.Join(company, "skills", "company-only"), "company-only", "company skill")
	mustWriteJSON(t, filepath.Join(company, "upstream-sources.json"), UpstreamManifest{Sources: []Source{
		{Repository: "official/skills", URL: "https://github.com/official/skills", Ref: "dev", Skills: []ManifestSkill{
			{Name: "duplicate", Path: "skills/duplicate/SKILL.md"},
			{Name: "official-only", Path: "skills/official-only/SKILL.md"},
		}},
	}})

	mustWriteSkill(t, filepath.Join(official, "skills", "duplicate"), "duplicate", "official skill")
	mustWriteSkill(t, filepath.Join(official, "skills", "official-only"), "official-only", "official only")

	catalog, err := LoadCatalog(personal, ghq)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Skills) != 4 {
		t.Fatalf("got %d skills, want 4", len(catalog.Skills))
	}
	byName := make(map[string]Skill)
	for _, skill := range catalog.Skills {
		byName[skill.Name] = skill
	}
	if got := byName["duplicate"]; got.Repository != "official/skills" || got.Ref != "dev" {
		t.Fatalf("duplicate resolved to %+v", got)
	}
	if len(catalog.Collisions) != 1 || catalog.Collisions[0].Ignored.Repository != "company/skills" {
		t.Fatalf("unexpected collisions: %+v", catalog.Collisions)
	}
}

func initTestRepo(t *testing.T, path, remote string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-b", "main"}, {"remote", "add", "origin", remote}} {
		command := exec.Command("git", args...)
		command.Dir = path
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
}

func mustWriteSkill(t *testing.T, dir, name, description string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: " + description + "\n---\n\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustWriteJSON(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}
