package skills

import (
	"crypto/md5"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/enough/enough/backend/enoughhome"
)

//go:embed bundled optional
var BundledFS embed.FS

type SyncResult struct {
	Copied                      []string `json:"copied"`
	Updated                     []string `json:"updated"`
	Skipped                     int      `json:"skipped"`
	UserModified                []string `json:"user_modified"`
	Cleaned                     []string `json:"cleaned"`
	Suppressed                  []string `json:"suppressed"`
	TotalBundled                int      `json:"total_bundled"`
	OptionalProvenanceBackfilled []string `json:"optional_provenance_backfilled"`
	SkippedOptOut               bool     `json:"skipped_opt_out,omitempty"`
}

func ReadManifest() map[string]string {
	manifest := make(map[string]string)
	path := filepath.Join(SkillsDir(), ".bundled_manifest")
	data, err := os.ReadFile(path)
	if err != nil {
		return manifest
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if idx := strings.Index(line, ":"); idx >= 0 {
			name := strings.TrimSpace(line[:idx])
			hash := strings.TrimSpace(line[idx+1:])
			manifest[name] = hash
		} else {
			manifest[line] = ""
		}
	}
	return manifest
}

func writeManifest(manifest map[string]string) error {
	path := filepath.Join(SkillsDir(), ".bundled_manifest")
	var lines []string
	for k, v := range manifest {
		lines = append(lines, fmt.Sprintf("%s:%s", k, v))
	}
	sort.Strings(lines)
	data := strings.Join(lines, "\n") + "\n"
	return atomicWrite(path, []byte(data))
}

func computeDirHashOnDisk(dirPath string) string {
	h := md5.New()
	var paths []string
	_ = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	sort.Strings(paths)
	for _, p := range paths {
		rel, err := filepath.Rel(dirPath, p)
		if err != nil {
			continue
		}
		h.Write([]byte(filepath.ToSlash(rel)))
		data, err := os.ReadFile(p)
		if err == nil {
			h.Write(data)
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func computeDirHashEmbedded(fs embed.FS, dirPath string) string {
	h := md5.New()
	var paths []string
	var walk func(string)
	walk = func(current string) {
		entries, err := fs.ReadDir(current)
		if err != nil {
			return
		}
		for _, entry := range entries {
			full := path.Join(current, entry.Name())
			if entry.IsDir() {
				walk(full)
			} else {
				paths = append(paths, full)
			}
		}
	}
	walk(dirPath)
	sort.Strings(paths)
	for _, p := range paths {
		rel, err := filepath.Rel(dirPath, p)
		if err != nil {
			continue
		}
		h.Write([]byte(filepath.ToSlash(rel)))
		data, err := fs.ReadFile(p)
		if err == nil {
			h.Write(data)
		}
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func readSkillNameFromEmbed(fs embed.FS, skillMdPath, fallback string) string {
	data, err := fs.ReadFile(skillMdPath)
	if err != nil {
		return fallback
	}
	fm, _ := ParseFrontmatter(string(data))
	if fm != nil {
		if name, ok := fm["name"].(string); ok && name != "" {
			return name
		}
	}
	return fallback
}

func copyEmbeddedDir(fs embed.FS, srcDir, destDir string) error {
	var walk func(string, string) error
	walk = func(src, dest string) error {
		entries, err := fs.ReadDir(src)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(dest, 0o700); err != nil {
			return err
		}
		for _, entry := range entries {
			srcPath := path.Join(src, entry.Name())
			destPath := filepath.Join(dest, entry.Name())
			if entry.IsDir() {
				if err := walk(srcPath, destPath); err != nil {
					return err
				}
			} else {
				data, err := fs.ReadFile(srcPath)
				if err != nil {
					return err
				}
				if err := atomicWrite(destPath, data); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(srcDir, destDir)
}

func rmtreeWritable(dirPath string) error {
	_ = filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err == nil {
			_ = os.Chmod(path, 0o777)
		}
		return nil
	})
	return os.RemoveAll(dirPath)
}

func SyncSkills(quiet bool) (SyncResult, error) {
	home := enoughhome.HomeDir()
	if _, err := os.Stat(filepath.Join(home, ".no-bundled-skills")); err == nil {
		if !quiet {
			fmt.Println("  (skipped — profile opted out of bundled skills via .no-bundled-skills)")
		}
		return SyncResult{
			Copied:        []string{},
			Updated:       []string{},
			UserModified:  []string{},
			Cleaned:       []string{},
			SkippedOptOut: true,
		}, nil
	}

	skillsDir := SkillsDir()
	if err := os.MkdirAll(skillsDir, 0o700); err != nil {
		return SyncResult{}, err
	}

	manifest := ReadManifest()
	suppressed := loadSuppressed()

	var bundledSkills []struct {
		Name     string
		SrcDir   string
		Category string
	}

	_ = fs.WalkDir(BundledFS, "bundled", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && d.Name() == "SKILL.md" {
			srcDir := path.Dir(p)
			fallback := path.Base(srcDir)
			name := readSkillNameFromEmbed(BundledFS, p, fallback)
			// Compute category relative to "bundled"
			cat := computeSkillCategory(p, "bundled")
			bundledSkills = append(bundledSkills, struct {
				Name     string
				SrcDir   string
				Category string
			}{
				Name:     name,
				SrcDir:   srcDir,
				Category: cat,
			})
		}
		return nil
	})

	copied := []string{}
	updated := []string{}
	userModified := []string{}
	suppressedSkipped := []string{}
	skipped := 0

	bundledNames := make(map[string]bool)

	for _, bSkill := range bundledSkills {
		bundledNames[bSkill.Name] = true

		if suppressed[bSkill.Name] {
			suppressedSkipped = append(suppressedSkipped, bSkill.Name)
			continue
		}

		destRel := strings.TrimPrefix(bSkill.SrcDir, "bundled/")
		dest := filepath.Join(skillsDir, destRel)
		bundledHash := computeDirHashEmbedded(BundledFS, bSkill.SrcDir)

		originHash, inManifest := manifest[bSkill.Name]
		destExists := false
		if _, err := os.Stat(dest); err == nil {
			destExists = true
		}

		if !inManifest {
			if destExists {
				userHash := computeDirHashOnDisk(dest)
				if userHash == bundledHash {
					manifest[bSkill.Name] = bundledHash
					skipped++
				} else {
					skipped++
					if !quiet {
						fmt.Printf("  ⚠ %s: bundled version shipped but you already have a local skill by this name — yours was kept. Run `enough skills reset %s` to replace it with the bundled version.\n", bSkill.Name, bSkill.Name)
					}
				}
			} else {
				if err := copyEmbeddedDir(BundledFS, bSkill.SrcDir, dest); err == nil {
					copied = append(copied, bSkill.Name)
					manifest[bSkill.Name] = bundledHash
					if !quiet {
						fmt.Printf("  + %s\n", bSkill.Name)
					}
				} else {
					if !quiet {
						fmt.Printf("  ! Failed to copy %s: %v\n", bSkill.Name, err)
					}
				}
			}
		} else if destExists {
			userHash := computeDirHashOnDisk(dest)
			if originHash == "" {
				// v1 migration
				manifest[bSkill.Name] = userHash
				skipped++
				continue
			}

			if userHash != originHash {
				userModified = append(userModified, bSkill.Name)
				if !quiet {
					fmt.Printf("  ~ %s (user-modified, skipping)\n", bSkill.Name)
				}
				continue
			}

			if bundledHash != originHash {
				backup := dest + ".bak"
				_ = rmtreeWritable(backup)
				if err := os.Rename(dest, backup); err == nil {
					if err := copyEmbeddedDir(BundledFS, bSkill.SrcDir, dest); err == nil {
						manifest[bSkill.Name] = bundledHash
						updated = append(updated, bSkill.Name)
						if !quiet {
							fmt.Printf("  ↑ %s (updated)\n", bSkill.Name)
						}
						_ = rmtreeWritable(backup)
					} else {
						_ = os.Rename(backup, dest)
						if !quiet {
							fmt.Printf("  ! Failed to update %s: %v\n", bSkill.Name, err)
						}
					}
				} else {
					if !quiet {
						fmt.Printf("  ! Failed to update %s: backup failed\n", bSkill.Name)
					}
				}
			} else {
				skipped++
			}
		} else {
			// Deleted by user
			skipped++
		}
	}

	// Clean manifest entries removed from bundled
	var cleaned []string
	for name := range manifest {
		if !bundledNames[name] {
			cleaned = append(cleaned, name)
		}
	}
	for _, name := range cleaned {
		delete(manifest, name)
	}
	sort.Strings(cleaned)

	// Copy category DESCRIPTION.md files
	_ = fs.WalkDir(BundledFS, "bundled", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && d.Name() == "DESCRIPTION.md" {
			destRel := strings.TrimPrefix(p, "bundled/")
			destDesc := filepath.Join(skillsDir, destRel)
			if _, err := os.Stat(destDesc); os.IsNotExist(err) {
				_ = os.MkdirAll(filepath.Dir(destDesc), 0o700)
				data, err := BundledFS.ReadFile(p)
				if err == nil {
					_ = atomicWrite(destDesc, data)
				}
			}
		}
		return nil
	})

	_ = writeManifest(manifest)

	return SyncResult{
		Copied:       copied,
		Updated:      updated,
		Skipped:      skipped,
		UserModified: userModified,
		Cleaned:      cleaned,
		Suppressed:   suppressedSkipped,
		TotalBundled: len(bundledSkills),
	}, nil
}

func ResetBundledSkill(name string, restore bool) (bool, string, *SyncResult, error) {
	manifest := ReadManifest()
	bundledSkills := make(map[string]string)

	_ = fs.WalkDir(BundledFS, "bundled", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && d.Name() == "SKILL.md" {
			srcDir := path.Dir(p)
			fallback := path.Base(srcDir)
			skillName := readSkillNameFromEmbed(BundledFS, p, fallback)
			bundledSkills[skillName] = srcDir
		}
		return nil
	})

	_, inManifest := manifest[name]
	srcDir, isBundled := bundledSkills[name]

	if !inManifest && !isBundled {
		return false, fmt.Sprintf("'%s' is not a tracked bundled skill. Nothing to reset.", name), nil, nil
	}

	if restore {
		if !isBundled {
			return false, fmt.Sprintf("'%s' has no bundled source — manifest entry preserved but cannot restore from bundled.", name), nil, nil
		}
		destRel := strings.TrimPrefix(srcDir, "bundled/")
		dest := filepath.Join(SkillsDir(), destRel)
		if _, err := os.Stat(dest); err == nil {
			if err := rmtreeWritable(dest); err != nil {
				return false, fmt.Sprintf("Could not delete user copy at %s: %v. Manifest entry preserved.", dest, err), nil, nil
			}
		}
	}

	if inManifest {
		delete(manifest, name)
		_ = writeManifest(manifest)
	}

	synced, err := SyncSkills(true)
	if err != nil {
		return false, "Reset manifest cleared but sync failed: " + err.Error(), nil, err
	}

	message := fmt.Sprintf("Cleared manifest entry for '%s'. Future updates will re-baseline against your copy.", name)
	if restore {
		message = fmt.Sprintf("Restored '%s' from bundled source.", name)
	}
	return true, message, &synced, nil
}
