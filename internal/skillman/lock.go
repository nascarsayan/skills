package skillman

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type LocalLock struct {
	Version int                       `json:"version"`
	Skills  map[string]LocalLockEntry `json:"skills"`
}

type LocalLockEntry struct {
	Source       string `json:"source"`
	SourceURL    string `json:"sourceUrl,omitempty"`
	Ref          string `json:"ref,omitempty"`
	SourceType   string `json:"sourceType"`
	SkillPath    string `json:"skillPath,omitempty"`
	ComputedHash string `json:"computedHash"`
}

func ReadLocalLock(repoRoot string) (LocalLock, error) {
	path := filepath.Join(repoRoot, "skills-lock.json")
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return LocalLock{Version: 1, Skills: make(map[string]LocalLockEntry)}, nil
		}
		return LocalLock{}, err
	}
	var lock LocalLock
	if err := json.Unmarshal(content, &lock); err != nil {
		return LocalLock{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if lock.Version != 1 || lock.Skills == nil {
		return LocalLock{}, fmt.Errorf("unsupported skills-lock.json version %d", lock.Version)
	}
	return lock, nil
}

func SetLocalLockSkill(repoRoot string, skill Skill, enabled bool) error {
	lock, err := ReadLocalLock(repoRoot)
	if err != nil {
		return err
	}
	if enabled {
		hash, err := SkillFolderHash(skill.Directory)
		if err != nil {
			return err
		}
		lock.Skills[skill.Name] = LocalLockEntry{
			Source:       skill.Repository,
			SourceURL:    githubCloneURL(skill),
			Ref:          skill.Ref,
			SourceType:   "github",
			SkillPath:    skill.SkillPath,
			ComputedHash: hash,
		}
	} else {
		delete(lock.Skills, skill.Name)
	}
	return writeLocalLock(repoRoot, lock)
}

func ReplaceLocalLock(repoRoot string, skills []Skill) error {
	lock := LocalLock{Version: 1, Skills: make(map[string]LocalLockEntry, len(skills))}
	for _, skill := range skills {
		hash, err := SkillFolderHash(skill.Directory)
		if err != nil {
			return err
		}
		lock.Skills[skill.Name] = LocalLockEntry{
			Source:       skill.Repository,
			SourceURL:    githubCloneURL(skill),
			Ref:          skill.Ref,
			SourceType:   "github",
			SkillPath:    skill.SkillPath,
			ComputedHash: hash,
		}
	}
	return writeLocalLock(repoRoot, lock)
}

func writeLocalLock(repoRoot string, lock LocalLock) error {
	orderedNames := make([]string, 0, len(lock.Skills))
	for name := range lock.Skills {
		orderedNames = append(orderedNames, name)
	}
	sort.Strings(orderedNames)
	ordered := make(map[string]LocalLockEntry, len(orderedNames))
	for _, name := range orderedNames {
		ordered[name] = lock.Skills[name]
	}
	lock.Skills = ordered

	content, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return os.WriteFile(filepath.Join(repoRoot, "skills-lock.json"), content, 0o644)
}

func SkillFolderHash(skillDir string) (string, error) {
	type fileRecord struct {
		path    string
		content []byte
	}
	var files []fileRecord
	err := filepath.WalkDir(skillDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(skillDir, path)
		if err != nil {
			return err
		}
		files = append(files, fileRecord{path: filepath.ToSlash(relative), content: content})
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })

	hash := sha256.New()
	for _, file := range files {
		_, _ = hash.Write([]byte(file.path))
		_, _ = hash.Write(file.content)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func githubCloneURL(skill Skill) string {
	if skill.URL != "" {
		return strings.TrimSuffix(skill.URL, ".git") + ".git"
	}
	return "https://github.com/" + skill.Repository + ".git"
}
