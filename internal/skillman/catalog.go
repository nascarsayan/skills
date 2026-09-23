package skillman

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type UpstreamManifest struct {
	Sources []Source `json:"sources"`
}

type Source struct {
	Repository string          `json:"repository"`
	URL        string          `json:"url"`
	Ref        string          `json:"ref"`
	Skills     []ManifestSkill `json:"skills"`
}

type ManifestSkill struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Repository  string `json:"repository"`
	URL         string `json:"url"`
	Ref         string `json:"ref"`
	SkillPath   string `json:"skillPath"`
	Directory   string `json:"directory"`
	RepoPath    string `json:"repoPath"`
	Warning     string `json:"warning,omitempty"`
}

func (s Skill) InstallSource() string {
	if s.Ref == "" {
		return s.Repository
	}
	return s.Repository + "#" + s.Ref
}

type Collision struct {
	Name    string
	Kept    Skill
	Ignored Skill
}

type Catalog struct {
	Skills     []Skill
	Sources    []Source
	Collisions []Collision
}

func LoadCatalog(repoRoot, ghqRoot string) (Catalog, error) {
	identity, ref, url, err := repositoryIdentity(repoRoot)
	if err != nil {
		return Catalog{}, err
	}

	loader := catalogLoader{
		ghqRoot:            ghqRoot,
		skills:             make(map[string]Skill),
		visitedCollections: make(map[string]bool),
	}
	if err := loader.loadCollection(repoRoot, Source{Repository: identity, URL: url, Ref: ref}); err != nil {
		return Catalog{}, err
	}

	names := make([]string, 0, len(loader.skills))
	for name := range loader.skills {
		names = append(names, name)
	}
	sort.Strings(names)

	catalog := Catalog{Sources: loader.sources, Collisions: loader.collisions}
	for _, name := range names {
		catalog.Skills = append(catalog.Skills, loader.skills[name])
	}
	return catalog, nil
}

type catalogLoader struct {
	ghqRoot            string
	skills             map[string]Skill
	sources            []Source
	collisions         []Collision
	visitedCollections map[string]bool
}

func (l *catalogLoader) loadCollection(repoPath string, source Source) error {
	realPath, err := filepath.EvalSymlinks(repoPath)
	if err != nil {
		return fmt.Errorf("resolve collection %s: %w", repoPath, err)
	}
	if l.visitedCollections[realPath] {
		return nil
	}
	l.visitedCollections[realPath] = true

	manifestPath := filepath.Join(realPath, "upstream-sources.json")
	manifest, err := readUpstreamManifest(manifestPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		for _, upstream := range manifest.Sources {
			upstream.Ref = normalizeRef(upstream.Ref)
			l.sources = append(l.sources, upstream)
			upstreamPath := filepath.Join(l.ghqRoot, repositoryGHQPath(upstream.Repository))
			if _, statErr := os.Stat(upstreamPath); statErr != nil {
				return fmt.Errorf("upstream %s is not cloned at %s", upstream.Repository, upstreamPath)
			}
			if _, nestedErr := os.Stat(filepath.Join(upstreamPath, "upstream-sources.json")); nestedErr == nil {
				if err := l.loadCollection(upstreamPath, upstream); err != nil {
					return err
				}
				continue
			}
			for _, declared := range upstream.Skills {
				if err := l.addManifestSkill(upstreamPath, upstream, declared); err != nil {
					return err
				}
			}
		}
	}

	matches, err := filepath.Glob(filepath.Join(realPath, "skills", "*", "SKILL.md"))
	if err != nil {
		return fmt.Errorf("scan owned skills in %s: %w", realPath, err)
	}
	sort.Strings(matches)
	for _, manifest := range matches {
		rel, err := filepath.Rel(realPath, manifest)
		if err != nil {
			return err
		}
		if err := l.addSkill(realPath, source, filepath.ToSlash(rel), ""); err != nil {
			return err
		}
	}
	return nil
}

func (l *catalogLoader) addManifestSkill(repoPath string, source Source, declared ManifestSkill) error {
	if declared.Name == "" || declared.Path == "" {
		return fmt.Errorf("source %s has a skill without name or path", source.Repository)
	}
	return l.addSkill(repoPath, source, declared.Path, declared.Name)
}

func (l *catalogLoader) addSkill(repoPath string, source Source, skillPath, declaredName string) error {
	manifest := filepath.Join(repoPath, filepath.FromSlash(skillPath))
	name, description, parseErr := readSkillFrontmatter(manifest)
	warning := ""
	if parseErr != nil {
		if declaredName == "" {
			return fmt.Errorf("read skill from %s: %w", source.Repository, parseErr)
		}
		name = declaredName
		description = "Upstream manifest requires repair before npx installation."
		warning = parseErr.Error()
	}
	if declaredName != "" && name != declaredName {
		return fmt.Errorf("manifest %s declares %q but catalog expects %q", manifest, name, declaredName)
	}

	skill := Skill{
		Name:        name,
		Description: description,
		Repository:  trimGitHubHost(source.Repository),
		URL:         source.URL,
		Ref:         normalizeRef(source.Ref),
		SkillPath:   filepath.ToSlash(skillPath),
		Directory:   filepath.Dir(manifest),
		RepoPath:    repoPath,
		Warning:     warning,
	}
	if skill.URL == "" {
		skill.URL = "https://github.com/" + skill.Repository
	}

	if kept, exists := l.skills[name]; exists {
		if kept.Directory != skill.Directory {
			l.collisions = append(l.collisions, Collision{Name: name, Kept: kept, Ignored: skill})
		}
		return nil
	}
	l.skills[name] = skill
	return nil
}

func readUpstreamManifest(path string) (UpstreamManifest, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return UpstreamManifest{}, err
	}
	var manifest UpstreamManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return UpstreamManifest{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return manifest, nil
}

func readSkillFrontmatter(path string) (string, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() || strings.TrimSpace(scanner.Text()) != "---" {
		return "", "", errors.New("missing YAML frontmatter")
	}
	var lines []string
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "---" {
			break
		}
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return "", "", err
	}

	var metadata struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(strings.Join(lines, "\n")), &metadata); err != nil {
		return "", "", err
	}
	if metadata.Name == "" || metadata.Description == "" {
		return "", "", errors.New("frontmatter requires name and description")
	}
	return metadata.Name, metadata.Description, nil
}

func repositoryIdentity(repoRoot string) (identity, ref, url string, err error) {
	remote, err := exec.Command("git", "-C", repoRoot, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", "", "", fmt.Errorf("read origin for %s: %w", repoRoot, err)
	}
	url = strings.TrimSpace(string(remote))
	identity = repositoryFromRemote(url)
	if identity == "" {
		return "", "", "", fmt.Errorf("unsupported GitHub origin %q", url)
	}
	branch, err := exec.Command("git", "-C", repoRoot, "branch", "--show-current").Output()
	if err != nil {
		return "", "", "", fmt.Errorf("read branch for %s: %w", repoRoot, err)
	}
	return identity, strings.TrimSpace(string(branch)), url, nil
}

func repositoryFromRemote(remote string) string {
	remote = strings.TrimSuffix(strings.TrimSpace(remote), ".git")
	for _, prefix := range []string{
		"https://github.com/",
		"http://github.com/",
		"git@github.com:",
		"ssh://git@github.com/",
	} {
		if strings.HasPrefix(remote, prefix) {
			return strings.TrimPrefix(remote, prefix)
		}
	}
	return ""
}

func trimGitHubHost(repository string) string {
	return strings.TrimPrefix(repository, "github.com/")
}

func repositoryGHQPath(repository string) string {
	if strings.Contains(repository, "/") && !strings.HasPrefix(repository, "github.com/") {
		return filepath.Join("github.com", filepath.FromSlash(repository))
	}
	return filepath.FromSlash(repository)
}

func normalizeRef(ref string) string {
	if ref == "default" {
		return ""
	}
	return ref
}
