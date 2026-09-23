package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nascarsayan/skills/internal/skillman"
)

type listedSkill struct {
	skillman.Skill
	State skillman.SkillState `json:"state"`
}

func main() {
	var (
		repoFlag  = flag.String("repo", "", "path to the private skill catalog and state repository")
		listFlag  = flag.Bool("list", false, "print the catalog and enablement state as JSON")
		checkFlag = flag.Bool("check", false, "validate skill state and harness principle links")
		syncFlag  = flag.Bool("sync", false, "fetch and fast-forward every upstream source")
		reconcile = flag.Bool("reconcile", false, "replace enabled skills and harness instructions with source symlinks")
	)
	flag.Parse()

	ctx := context.Background()
	runner := skillman.ExecRunner{}
	ghqRoot, err := skillman.GHQRoot(ctx, runner)
	fatalIf(err)
	repoRoot, err := findRepoRoot(*repoFlag, ghqRoot)
	fatalIf(err)

	fatalIf(skillman.EnsureSources(ctx, repoRoot, ghqRoot, *syncFlag, runner))
	catalog, err := skillman.LoadCatalog(repoRoot, ghqRoot)
	fatalIf(err)

	home, err := os.UserHomeDir()
	fatalIf(err)
	manager := &skillman.Manager{
		RepoRoot:  repoRoot,
		Harnesses: skillman.DefaultHarnesses(home),
		Runner:    runner,
	}
	principleSource := filepath.Join(repoRoot, "PRINCIPLES.md")
	principleTargets := skillman.DefaultPrincipleTargets(home)

	if *reconcile {
		linked, err := manager.Reconcile(catalog)
		fatalIf(err)
		principlesLinked, err := skillman.ReconcilePrinciples(principleSource, principleTargets)
		fatalIf(err)
		fmt.Printf("reconciled %d skill links and %d principles links; %d skills are available\n", linked, principlesLinked, len(catalog.Skills))
		return
	}
	if *checkFlag {
		fatalIf(checkLock(repoRoot, catalog, manager))
		fatalIf(skillman.CheckPrinciples(principleSource, principleTargets))
		fmt.Printf("validated %d available skills, skills-lock.json, and %d harness principle links\n", len(catalog.Skills), len(principleTargets))
		return
	}
	if *listFlag {
		fatalIf(printCatalog(catalog, manager))
		return
	}
	if *syncFlag {
		fmt.Printf("synced %d available skills from ghq sources\n", len(catalog.Skills))
		return
	}

	model, err := newTUIModel(catalog, manager, repoRoot, ghqRoot)
	fatalIf(err)
	_, err = tea.NewProgram(model, tea.WithAltScreen()).Run()
	fatalIf(err)
}

func findRepoRoot(explicit, ghqRoot string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	if configured := os.Getenv("SKILLMAN_REPO"); configured != "" {
		return filepath.Abs(configured)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	output, err := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err == nil {
		candidate := stringTrim(output)
		if isStateRepo(candidate) {
			return filepath.EvalSymlinks(candidate)
		}
	}
	return discoverStateRepo(ghqRoot)
}

func isStateRepo(path string) bool {
	for _, name := range []string{"upstream-sources.json", "skills-lock.json", "PRINCIPLES.md"} {
		if _, err := os.Stat(filepath.Join(path, name)); err != nil {
			return false
		}
	}
	return true
}

func discoverStateRepo(ghqRoot string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(ghqRoot, "*", "*", "*", "skills-lock.json"))
	if err != nil {
		return "", err
	}
	candidates := make([]string, 0, len(matches))
	for _, match := range matches {
		candidate := filepath.Dir(match)
		if isStateRepo(candidate) {
			candidates = append(candidates, candidate)
		}
	}
	switch len(candidates) {
	case 1:
		return filepath.EvalSymlinks(candidates[0])
	case 0:
		return "", fmt.Errorf("cannot find a skill state repository; pass --repo or set SKILLMAN_REPO")
	default:
		return "", fmt.Errorf("found multiple skill state repositories; pass --repo or set SKILLMAN_REPO")
	}
}

func printCatalog(catalog skillman.Catalog, manager *skillman.Manager) error {
	listed := make([]listedSkill, 0, len(catalog.Skills))
	for _, skill := range catalog.Skills {
		state, err := manager.SkillState(skill)
		if err != nil {
			return err
		}
		listed = append(listed, listedSkill{Skill: skill, State: state})
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(listed)
}

func checkLock(repoRoot string, catalog skillman.Catalog, manager *skillman.Manager) error {
	lock, err := skillman.ReadLocalLock(repoRoot)
	if err != nil {
		return err
	}
	available := make(map[string]skillman.Skill, len(catalog.Skills))
	enabled := make(map[string]bool)
	for _, skill := range catalog.Skills {
		available[skill.Name] = skill
		state, err := manager.SkillState(skill)
		if err != nil {
			return err
		}
		if state.Partial {
			return fmt.Errorf("skill %s is installed in only some harnesses; run skillman --reconcile", skill.Name)
		}
		if state.Enabled {
			if !state.Linked {
				return fmt.Errorf("enabled skill %s is not linked to its ghq source", skill.Name)
			}
			enabled[skill.Name] = true
			if _, ok := lock.Skills[skill.Name]; !ok {
				return fmt.Errorf("enabled skill %s is missing from skills-lock.json", skill.Name)
			}
		}
	}
	for name, entry := range lock.Skills {
		skill, ok := available[name]
		if !ok {
			return fmt.Errorf("locked skill %s has no upstream source", name)
		}
		if !enabled[name] {
			return fmt.Errorf("locked skill %s is disabled in every managed harness", name)
		}
		if entry.Source != skill.Repository || entry.SkillPath != skill.SkillPath {
			return fmt.Errorf("locked skill %s points to %s:%s, catalog resolves %s:%s", name, entry.Source, entry.SkillPath, skill.Repository, skill.SkillPath)
		}
	}
	return nil
}

func stringTrim(value []byte) string {
	for len(value) > 0 && (value[len(value)-1] == '\n' || value[len(value)-1] == '\r' || value[len(value)-1] == ' ') {
		value = value[:len(value)-1]
	}
	return string(value)
}

func fatalIf(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "skillman:", err)
	os.Exit(1)
}
