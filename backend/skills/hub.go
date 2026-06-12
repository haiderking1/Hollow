package skills

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Paths & Constants
// ---------------------------------------------------------------------------

func HubDir() string {
	return filepath.Join(SkillsDir(), ".hub")
}

func LockFilePath() string {
	return filepath.Join(HubDir(), "lock.json")
}

func QuarantineDir() string {
	return filepath.Join(HubDir(), "quarantine")
}

func AuditLogPath() string {
	return filepath.Join(HubDir(), "audit.log")
}

func TapsFilePath() string {
	return filepath.Join(HubDir(), "taps.json")
}

func IndexCacheDir() string {
	return filepath.Join(HubDir(), "index-cache")
}

const IndexCacheTTL = 3600 // 1 hour

// ---------------------------------------------------------------------------
// Data Models
// ---------------------------------------------------------------------------

type SkillMeta struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Source      string                 `json:"source"`
	Identifier  string                 `json:"identifier"`
	TrustLevel  string                 `json:"trust_level"`
	Repo        string                 `json:"repo,omitempty"`
	Path        string                 `json:"path,omitempty"`
	Tags        []string               `json:"tags"`
	Extra       map[string]interface{} `json:"extra"`
}

type SkillBundle struct {
	Name       string                 `json:"name"`
	Files      map[string][]byte      `json:"files"`
	Source     string                 `json:"source"`
	Identifier string                 `json:"identifier"`
	TrustLevel string                 `json:"trust_level"`
	Metadata   map[string]interface{} `json:"metadata"`
}

type SkillSource interface {
	Search(query string, limit int) []SkillMeta
	Fetch(identifier string) (*SkillBundle, error)
	Inspect(identifier string) (*SkillMeta, error)
	SourceID() string
	TrustLevelFor(identifier string) string
}

type OptionalSkillSource struct{}

func (s *OptionalSkillSource) SourceID() string {
	return "official"
}

func (s *OptionalSkillSource) TrustLevelFor(identifier string) string {
	return "builtin"
}

func (s *OptionalSkillSource) Search(query string, limit int) []SkillMeta {
	var results []SkillMeta
	queryLower := strings.ToLower(query)

	_ = fs.WalkDir(BundledFS, "optional", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && d.Name() == "SKILL.md" {
			data, err := BundledFS.ReadFile(p)
			if err != nil {
				return nil
			}
			fm, _ := ParseFrontmatter(string(data))
			if fm == nil {
				return nil
			}
			name, _ := fm["name"].(string)
			if name == "" {
				name = filepath.Base(filepath.Dir(p))
			}
			desc := extractSkillDescription(fm)
			tags := extractSkillTags(fm)

			searchable := strings.ToLower(name + " " + desc + " " + strings.Join(tags, " "))
			if strings.Contains(searchable, queryLower) {
				relPath := strings.TrimPrefix(filepath.Dir(p), "optional/")
				results = append(results, SkillMeta{
					Name:        name,
					Description: desc,
					Source:      "official",
					Identifier:  "official/" + relPath,
					TrustLevel:  "builtin",
					Path:        relPath,
					Tags:        tags,
				})
			}
		}
		return nil
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results
}

func (s *OptionalSkillSource) Fetch(identifier string) (*SkillBundle, error) {
	rel := strings.TrimPrefix(identifier, "official/")
	skillDir := "optional/" + rel

	// Check if directory exists in BundledFS
	_, err := BundledFS.ReadDir(skillDir)
	if err != nil {
		return nil, fmt.Errorf("optional skill not found: %s", identifier)
	}

	files := make(map[string][]byte)
	var walk func(string) error
	walk = func(dir string) error {
		subEntries, err := BundledFS.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range subEntries {
			fullPath := dir + "/" + entry.Name()
			if entry.IsDir() {
				if err := walk(fullPath); err != nil {
					return err
				}
			} else {
				data, err := BundledFS.ReadFile(fullPath)
				if err != nil {
					continue
				}
				relPath := strings.TrimPrefix(fullPath, skillDir+"/")
				files[relPath] = data
			}
		}
		return nil
	}

	if err := walk(skillDir); err != nil {
		return nil, err
	}

	name := filepath.Base(skillDir)

	return &SkillBundle{
		Name:       name,
		Files:      files,
		Source:     "official",
		Identifier: identifier,
		TrustLevel: "builtin",
	}, nil
}

func (s *OptionalSkillSource) Inspect(identifier string) (*SkillMeta, error) {
	rel := strings.TrimPrefix(identifier, "official/")
	skillMdPath := "optional/" + rel + "/SKILL.md"

	data, err := BundledFS.ReadFile(skillMdPath)
	if err != nil {
		return nil, err
	}

	fm, _ := ParseFrontmatter(string(data))
	if fm == nil {
		return nil, fmt.Errorf("invalid frontmatter in optional skill SKILL.md")
	}

	name, _ := fm["name"].(string)
	if name == "" {
		name = filepath.Base(filepath.Dir(skillMdPath))
	}
	desc := extractSkillDescription(fm)
	tags := extractSkillTags(fm)

	return &SkillMeta{
		Name:        name,
		Description: desc,
		Source:      "official",
		Identifier:  identifier,
		TrustLevel:  "builtin",
		Path:        rel,
		Tags:        tags,
	}, nil
}

// ---------------------------------------------------------------------------
// Path validation and Traversal protection
// ---------------------------------------------------------------------------

func normalizeBundlePath(val string, fieldName string, allowNested bool) (string, error) {
	raw := strings.TrimSpace(val)
	if raw == "" {
		return "", fmt.Errorf("unsafe %s: empty path", fieldName)
	}

	normalized := strings.ReplaceAll(raw, "\\", "/")
	parts := strings.Split(normalized, "/")

	var cleanParts []string
	for _, p := range parts {
		if p == "" || p == "." {
			continue
		}
		if p == ".." {
			return "", fmt.Errorf("unsafe %s: contains traversal segment: %s", fieldName, val)
		}
		cleanParts = append(cleanParts, p)
	}

	if len(cleanParts) == 0 {
		return "", fmt.Errorf("unsafe %s: empty path", fieldName)
	}

	// Traversal checks
	if strings.HasPrefix(normalized, "/") || filepath.IsAbs(raw) {
		return "", fmt.Errorf("unsafe %s: absolute path not allowed: %s", fieldName, val)
	}

	// Drive letter check (e.g. C:)
	if len(cleanParts[0]) == 2 && cleanParts[0][1] == ':' {
		return "", fmt.Errorf("unsafe %s: drive letter check failed: %s", fieldName, val)
	}

	if !allowNested && len(cleanParts) != 1 {
		return "", fmt.Errorf("unsafe %s: nested path not allowed: %s", fieldName, val)
	}

	return strings.Join(cleanParts, "/"), nil
}

func validateSkillName(name string) (string, error) {
	return normalizeBundlePath(name, "skill name", false)
}

func validateInstallParentPath(category string) (string, error) {
	return normalizeBundlePath(category, "install parent path", true)
}

func normalizeLockInstallPath(installPath string, skillName string) (string, error) {
	safeSkillName, err := validateSkillName(skillName)
	if err != nil {
		return "", err
	}
	normalized, err := normalizeBundlePath(installPath, "install path", true)
	if err != nil {
		return "", err
	}
	parts := strings.Split(normalized, "/")
	if len(parts) == 0 || parts[len(parts)-1] != safeSkillName {
		return "", fmt.Errorf("unsafe install path: final component must match skill name %q, got: %s", safeSkillName, installPath)
	}
	return normalized, nil
}

func isPathRedirect(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeSymlink != 0
}

func resolveLockInstallPath(installPath string, skillName string) (string, error) {
	normalized, err := normalizeLockInstallPath(installPath, skillName)
	if err != nil {
		return "", err
	}

	skillsRoot, err := filepath.Abs(SkillsDir())
	if err != nil {
		return "", err
	}

	target := skillsRoot
	parts := strings.Split(normalized, "/")
	for _, part := range parts {
		target = filepath.Join(target, part)
		if isPathRedirect(target) {
			return "", fmt.Errorf("unsafe install path: path contains symlink redirect: %s", target)
		}
	}

	resolved, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}

	if resolved == skillsRoot {
		return "", fmt.Errorf("unsafe install path resolved to skills root: %s", installPath)
	}

	if !strings.HasPrefix(resolved, skillsRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe install path: escapes skills root: %s", installPath)
	}

	return resolved, nil
}

// ---------------------------------------------------------------------------
// SSRF & URL Safety
// ---------------------------------------------------------------------------

var blockedHostnames = map[string]bool{
	"metadata.google.internal": true,
	"metadata.goog":            true,
}

var alwaysBlockedIPs = map[string]bool{
	"169.254.169.254":       true,
	"169.254.170.2":         true,
	"169.254.169.253":       true,
	"fd00:ec2::254":         true,
	"100.100.100.200":       true,
	"::ffff:169.254.169.254": true,
	"::ffff:169.254.170.2":   true,
	"::ffff:169.254.169.253": true,
	"::ffff:100.100.100.200": true,
}

var _, cgnatNet, _ = net.ParseCIDR("100.64.0.0/10")
var _, linkLocalNet, _ = net.ParseCIDR("169.254.0.0/16")
var _, ipv4MappedLinkLocalNet, _ = net.ParseCIDR("::ffff:169.254.0.0/112")

func isSafeURL(urlStr string) bool {
	u, err := url.Parse(urlStr)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return false
	}
	if blockedHostnames[host] {
		return false
	}

	// check if user opted out of private-IP blocking via env var
	allowPrivate := false
	envVal := strings.ToLower(strings.TrimSpace(os.Getenv("ENOUGH_ALLOW_PRIVATE_URLS")))
	if envVal == "" {
		envVal = strings.ToLower(strings.TrimSpace(os.Getenv("HERMES_ALLOW_PRIVATE_URLS")))
	}
	if envVal == "true" || envVal == "1" || envVal == "yes" {
		allowPrivate = true
	}

	// literal IP checks
	if ip := net.ParseIP(host); ip != nil {
		if isAlwaysBlockedIP(ip) {
			return false
		}
		if !allowPrivate && isBlockedIP(ip) {
			return false
		}
		return true
	}

	// resolve DNS
	ips, err := net.LookupIP(host)
	if err != nil {
		return false
	}

	for _, ip := range ips {
		if isAlwaysBlockedIP(ip) {
			return false
		}
		if !allowPrivate && isBlockedIP(ip) {
			return false
		}
	}

	return true
}

func isAlwaysBlockedIP(ip net.IP) bool {
	ipStr := ip.String()
	if alwaysBlockedIPs[ipStr] {
		return true
	}
	if linkLocalNet.Contains(ip) || ipv4MappedLinkLocalNet.Contains(ip) {
		return true
	}
	return false
}

func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if ip.IsPrivate() {
		return true
	}
	if cgnatNet.Contains(ip) {
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// HTTP request helper with SSRF validation
// ---------------------------------------------------------------------------

var httpClient = &http.Client{
	Timeout: 20 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("stopped after 5 redirects")
		}
		if !isSafeURL(req.URL.String()) {
			return fmt.Errorf("unsafe redirect URL: %s", req.URL.String())
		}
		return nil
	},
}

func guardedHttpGet(urlStr string, headers map[string]string) ([]byte, int, error) {
	if !isSafeURL(urlStr) {
		return nil, 0, fmt.Errorf("blocked unsafe URL: %s", urlStr)
	}

	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return nil, 0, err
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}

	return body, resp.StatusCode, nil
}

// ---------------------------------------------------------------------------
// GitHub Auth
// ---------------------------------------------------------------------------

type GitHubAuth struct {
	token      string
	authMethod string
}

func ResolveGitHubAuth() GitHubAuth {
	// 1. Env vars
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	if token != "" {
		return GitHubAuth{token: token, authMethod: "pat"}
	}

	// 2. gh CLI
	token = tryGhCli()
	if token != "" {
		return GitHubAuth{token: token, authMethod: "gh-cli"}
	}

	return GitHubAuth{token: "", authMethod: "anonymous"}
}

func tryGhCli() string {
	cmd := exec.Command("gh", "auth", "token")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err == nil {
		return strings.TrimSpace(stdout.String())
	}
	return ""
}

func (g GitHubAuth) GetHeaders() map[string]string {
	headers := map[string]string{
		"Accept": "application/vnd.github.v3+json",
	}
	if g.token != "" {
		headers["Authorization"] = "token " + g.token
	}
	return headers
}

// ---------------------------------------------------------------------------
// Lock File Manager
// ---------------------------------------------------------------------------

type InstalledSkill struct {
	Source      string                 `json:"source"`
	Identifier  string                 `json:"identifier"`
	TrustLevel  string                 `json:"trust_level"`
	ScanVerdict string                 `json:"scan_verdict"`
	ContentHash string                 `json:"content_hash"`
	InstallPath string                 `json:"install_path"`
	Files       []string               `json:"files"`
	Metadata    map[string]interface{} `json:"metadata"`
	InstalledAt string                 `json:"installed_at"`
	UpdatedAt   string                 `json:"updated_at"`
}

type HubLockFile struct {
	Version   int                       `json:"version"`
	Installed map[string]InstalledSkill `json:"installed"`
}

func LoadLockFile() HubLockFile {
	lock := HubLockFile{
		Version:   1,
		Installed: make(map[string]InstalledSkill),
	}
	path := LockFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return lock
	}
	_ = json.Unmarshal(data, &lock)
	if lock.Installed == nil {
		lock.Installed = make(map[string]InstalledSkill)
	}
	return lock
}

func SaveLockFile(lock HubLockFile) error {
	path := LockFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(data, '\n'))
}

func (l *HubLockFile) RecordInstall(
	name string,
	source string,
	identifier string,
	trustLevel string,
	scanVerdict string,
	skillHash string,
	installPath string,
	files []string,
	metadata map[string]interface{},
) error {
	safeName, err := validateSkillName(name)
	if err != nil {
		return err
	}
	safeInstallPath, err := normalizeLockInstallPath(installPath, safeName)
	if err != nil {
		return err
	}

	lock := LoadLockFile()
	now := time.Now().UTC().Format(time.RFC3339)
	installedAt := now
	if existing, ok := lock.Installed[safeName]; ok {
		installedAt = existing.InstalledAt
	}

	lock.Installed[safeName] = InstalledSkill{
		Source:      source,
		Identifier:  identifier,
		TrustLevel:  trustLevel,
		ScanVerdict: scanVerdict,
		ContentHash: skillHash,
		InstallPath: safeInstallPath,
		Files:       files,
		Metadata:    metadata,
		InstalledAt: installedAt,
		UpdatedAt:   now,
	}
	return SaveLockFile(lock)
}

func (l *HubLockFile) RecordUninstall(name string) error {
	lock := LoadLockFile()
	delete(lock.Installed, name)
	return SaveLockFile(lock)
}

func (l *HubLockFile) GetInstalled(name string) *InstalledSkill {
	lock := LoadLockFile()
	if entry, ok := lock.Installed[name]; ok {
		return &entry
	}
	return nil
}

func (l *HubLockFile) ListInstalled() []InstalledSkill {
	lock := LoadLockFile()
	var out []InstalledSkill
	for name, entry := range lock.Installed {
		// Include the name in the record (since it's the map key)
		entry.Metadata = map[string]interface{}{
			"name": name,
		}
		// merge other metadata
		out = append(out, entry)
	}
	return out
}

// ---------------------------------------------------------------------------
// Taps Manager
// ---------------------------------------------------------------------------

type TapEntry struct {
	Repo string `json:"repo"`
	Path string `json:"path"`
}

type TapsFile struct {
	Taps []TapEntry `json:"taps"`
}

func LoadTaps() []TapEntry {
	path := TapsFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var tf TapsFile
	if err := json.Unmarshal(data, &tf); err != nil {
		return nil
	}
	return tf.Taps
}

func SaveTaps(taps []TapEntry) error {
	path := TapsFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(TapsFile{Taps: taps}, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(data, '\n'))
}

func AddTap(repo string, pathVal string) bool {
	if pathVal == "" {
		pathVal = "skills/"
	}
	taps := LoadTaps()
	for _, t := range taps {
		if t.Repo == repo {
			return false
		}
	}
	taps = append(taps, TapEntry{Repo: repo, Path: pathVal})
	_ = SaveTaps(taps)
	return true
}

func RemoveTap(repo string) bool {
	taps := LoadTaps()
	var newTaps []TapEntry
	found := false
	for _, t := range taps {
		if t.Repo == repo {
			found = true
			continue
		}
		newTaps = append(newTaps, t)
	}
	if !found {
		return false
	}
	_ = SaveTaps(newTaps)
	return true
}

// ---------------------------------------------------------------------------
// Audit Logging
// ---------------------------------------------------------------------------

func appendAuditLog(action, skillName, source, trustLevel, verdict, extra string) {
	_ = os.MkdirAll(filepath.Dir(AuditLogPath()), 0o700)
	timestamp := time.Now().UTC().Format(time.RFC3339)
	line := fmt.Sprintf("%s %s %s %s:%s %s", timestamp, action, skillName, source, trustLevel, verdict)
	if extra != "" {
		line += " " + extra
	}
	line += "\n"

	f, err := os.OpenFile(AuditLogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		_, _ = f.WriteString(line)
		f.Close()
	}
}

// ---------------------------------------------------------------------------
// Index Cache
// ---------------------------------------------------------------------------

func readIndexCache(key string) ([]byte, bool) {
	path := filepath.Join(IndexCacheDir(), key+".json")
	fi, err := os.Stat(path)
	if err != nil {
		return nil, false
	}
	if time.Since(fi.ModTime()) > IndexCacheTTL*time.Second {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return data, true
}

func writeIndexCache(key string, data []byte) {
	_ = os.MkdirAll(IndexCacheDir(), 0o700)
	ignoreFile := filepath.Join(HubDir(), ".ignore")
	if _, err := os.Stat(ignoreFile); os.IsNotExist(err) {
		_ = os.WriteFile(ignoreFile, []byte("# Exclude hub internals from search tools\n*\n"), 0o600)
	}
	path := filepath.Join(IndexCacheDir(), key+".json")
	_ = atomicWrite(path, data)
}

// ---------------------------------------------------------------------------
// Hub Operations (quarantine, install, uninstall)
// ---------------------------------------------------------------------------

func ensureHubDirs() {
	_ = os.MkdirAll(HubDir(), 0o700)
	_ = os.MkdirAll(QuarantineDir(), 0o700)
	_ = os.MkdirAll(IndexCacheDir(), 0o700)

	lockPath := LockFilePath()
	if _, err := os.Stat(lockPath); os.IsNotExist(err) {
		_ = os.WriteFile(lockPath, []byte("{\n  \"version\": 1,\n  \"installed\": {}\n}\n"), 0o600)
	}

	auditPath := AuditLogPath()
	if _, err := os.Stat(auditPath); os.IsNotExist(err) {
		_ = os.WriteFile(auditPath, []byte(""), 0o600)
	}

	tapsPath := TapsFilePath()
	if _, err := os.Stat(tapsPath); os.IsNotExist(err) {
		_ = os.WriteFile(tapsPath, []byte("{\n  \"taps\": []\n}\n"), 0o600)
	}
}

func quarantineBundle(bundle *SkillBundle) (string, error) {
	ensureHubDirs()
	safeName, err := validateSkillName(bundle.Name)
	if err != nil {
		return "", err
	}

	dest := filepath.Join(QuarantineDir(), safeName)
	_ = os.RemoveAll(dest)
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return "", err
	}

	for relPath, content := range bundle.Files {
		safeRel, err := normalizeBundlePath(relPath, "bundle file path", true)
		if err != nil {
			_ = os.RemoveAll(dest)
			return "", err
		}
		fileDest := filepath.Join(dest, filepath.FromSlash(safeRel))
		if err := os.MkdirAll(filepath.Dir(fileDest), 0o700); err != nil {
			_ = os.RemoveAll(dest)
			return "", err
		}
		if err := atomicWrite(fileDest, content); err != nil {
			_ = os.RemoveAll(dest)
			return "", err
		}
	}

	return dest, nil
}

func installFromQuarantine(
	quarantinePath string,
	skillName string,
	category string,
	bundle *SkillBundle,
	scanResult SkillScanResult,
) (string, error) {
	safeSkillName, err := validateSkillName(skillName)
	if err != nil {
		return "", err
	}
	safeCategory := ""
	if category != "" {
		var err error
		safeCategory, err = validateInstallParentPath(category)
		if err != nil {
			return "", err
		}
	}

	qResolved, err := filepath.Abs(quarantinePath)
	if err != nil {
		return "", err
	}
	qRoot, err := filepath.Abs(QuarantineDir())
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(qResolved, qRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe quarantine path: %s", quarantinePath)
	}

	installRelPath := safeSkillName
	if safeCategory != "" {
		installRelPath = safeCategory + "/" + safeSkillName
	}

	installDir, err := resolveLockInstallPath(installRelPath, safeSkillName)
	if err != nil {
		return "", err
	}

	_ = os.RemoveAll(installDir)

	// symlink check inside quarantine
	err = filepath.Walk(quarantinePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if isPathRedirect(path) {
			rel, _ := filepath.Rel(qResolved, path)
			return fmt.Errorf("installed skill contains symlinks, which is not allowed: %s", rel)
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(installDir), 0o700); err != nil {
		return "", err
	}

	if err := os.Rename(quarantinePath, installDir); err != nil {
		// fall back to copy if rename fails across devices
		if err := copyDir(quarantinePath, installDir); err != nil {
			return "", err
		}
		_ = os.RemoveAll(quarantinePath)
	}

	// Record in lock
	var fileList []string
	for f := range bundle.Files {
		fileList = append(fileList, f)
	}
	sort.Strings(fileList)

	lock := HubLockFile{}
	_ = lock.RecordInstall(
		safeSkillName,
		bundle.Source,
		bundle.Identifier,
		bundle.TrustLevel,
		scanResult.Verdict,
		computeDirHashOnDisk(installDir),
		strings.ReplaceAll(filepath.Clean(installDir[len(SkillsDir()):]), string(filepath.Separator), "/"),
		fileList,
		bundle.Metadata,
	)

	appendAuditLog(
		"INSTALL", safeSkillName, bundle.Source,
		bundle.TrustLevel, scanResult.Verdict,
		computeDirHashOnDisk(installDir),
	)

	return installDir, nil
}

func copyDir(src string, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

func UninstallSkill(skillName string) (bool, string) {
	lock := HubLockFile{}
	entry := lock.GetInstalled(skillName)
	if entry == nil {
		return false, fmt.Sprintf("'%s' is not a hub-installed skill (may be a builtin)", skillName)
	}

	installPath, err := resolveLockInstallPath(entry.InstallPath, skillName)
	if err != nil {
		return false, fmt.Sprintf("Refusing to uninstall '%s': %v", skillName, err)
	}

	if _, err := os.Stat(installPath); err == nil {
		_ = os.RemoveAll(installPath)
	}

	_ = lock.RecordUninstall(skillName)
	appendAuditLog("UNINSTALL", skillName, entry.Source, entry.TrustLevel, "n/a", "user_request")

	return true, fmt.Sprintf("Uninstalled '%s' from %s", skillName, entry.InstallPath)
}

func bundleContentHash(bundle *SkillBundle) string {
	h := sha256.New()
	var keys []string
	for k := range bundle.Files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write(bundle.Files[k])
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil))[:23]
}

// ---------------------------------------------------------------------------
// General Source Router
// ---------------------------------------------------------------------------

func CreateSourceRouter(auth *GitHubAuth) []SkillSource {
	var g GitHubAuth
	if auth == nil {
		g = ResolveGitHubAuth()
	} else {
		g = *auth
	}

	return []SkillSource{
		&OptionalSkillSource{},
		&HermesIndexSource{auth: g},
		&SkillsShSource{auth: g},
		&WellKnownSkillSource{},
		&UrlSource{},
		&GitHubSource{auth: g},
		&ClawHubSource{},
		&LobeHubSource{},
		&BrowseShSource{},
	}
}

// ---------------------------------------------------------------------------
// Unified Search
// ---------------------------------------------------------------------------

func UnifiedSearch(query string, sources []SkillSource, sourceFilter string, limit int) []SkillMeta {
	var all []SkillMeta
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Filter by source
	var active []SkillSource
	for _, src := range sources {
		sid := src.SourceID()
		if sourceFilter != "all" && sid != sourceFilter && sid != "official" {
			continue
		}
		active = append(active, src)
	}

	for _, src := range active {
		wg.Add(1)
		go func(s SkillSource) {
			defer wg.Done()
			results := s.Search(query, limit)
			mu.Lock()
			all = append(all, results...)
			mu.Unlock()
		}(src)
	}

	wg.Wait()

	// Deduplicate by identifier, prioritizing trust rank
	trustRank := map[string]int{
		"builtin":   2,
		"trusted":   1,
		"community": 0,
	}

	seen := make(map[string]SkillMeta)
	for _, r := range all {
		existing, ok := seen[r.Identifier]
		if !ok {
			seen[r.Identifier] = r
		} else {
			if trustRank[r.TrustLevel] > trustRank[existing.TrustLevel] {
				seen[r.Identifier] = r
			}
		}
	}

	var deduped []SkillMeta
	for _, r := range seen {
		deduped = append(deduped, r)
	}

	// Sort: official first, then by trust, then name
	sort.Slice(deduped, func(i, j int) bool {
		rI, rJ := deduped[i], deduped[j]
		if (rI.Source == "official") != (rJ.Source == "official") {
			return rI.Source == "official"
		}
		if trustRank[rI.TrustLevel] != trustRank[rJ.TrustLevel] {
			return trustRank[rI.TrustLevel] > trustRank[rJ.TrustLevel]
		}
		return strings.ToLower(rI.Name) < strings.ToLower(rJ.Name)
	})

	if limit > 0 && len(deduped) > limit {
		deduped = deduped[:limit]
	}
	return deduped
}

func ResolveShortName(name string, sources []SkillSource) string {
	results := UnifiedSearch(name, sources, "all", 20)
	var exact []SkillMeta
	for _, r := range results {
		if strings.ToLower(r.Name) == strings.ToLower(name) {
			exact = append(exact, r)
		}
	}
	if len(exact) == 1 {
		return exact[0].Identifier
	}
	return ""
}

func CheckForSkillUpdates(name string, sources []SkillSource) []map[string]interface{} {
	lock := HubLockFile{}
	installed := lock.ListInstalled()
	if name != "" {
		var filtered []InstalledSkill
		for _, entry := range installed {
			// retrieve name from metadata
			n, _ := entry.Metadata["name"].(string)
			if n == name {
				filtered = append(filtered, entry)
			}
		}
		installed = filtered
	}

	var results []map[string]interface{}
	for _, entry := range installed {
		n, _ := entry.Metadata["name"].(string)
		identifier := entry.Identifier
		sourceName := entry.Source

		var bundle *SkillBundle
		var fetchErr error
		for _, src := range sources {
			if src.SourceID() == sourceName || (sourceName == "skills-sh" && src.SourceID() == "skills.sh") {
				bundle, fetchErr = src.Fetch(identifier)
				if fetchErr == nil && bundle != nil {
					break
				}
			}
		}

		if bundle == nil {
			results = append(results, map[string]interface{}{
				"name":       n,
				"identifier": identifier,
				"source":     sourceName,
				"status":     "unavailable",
			})
			continue
		}

		currentHash := entry.ContentHash
		latestHash := bundleContentHash(bundle)
		status := "up_to_date"
		if currentHash != latestHash {
			status = "update_available"
		}

		results = append(results, map[string]interface{}{
			"name":         n,
			"identifier":   identifier,
			"source":       sourceName,
			"status":       status,
			"current_hash": currentHash,
			"latest_hash":  latestHash,
			"bundle":       bundle,
		})
	}
	return results
}

func InstallSkill(identifier string, category string, nameOverride string, force bool, skipConfirm bool, auth *GitHubAuth) (string, error) {
	ensureHubDirs()
	var g GitHubAuth
	if auth != nil {
		g = *auth
	} else {
		g = ResolveGitHubAuth()
	}
	sources := CreateSourceRouter(&g)

	// Resolve short name if no slash is in identifier
	if !strings.Contains(identifier, "/") {
		resolved := ResolveShortName(identifier, sources)
		if resolved == "" {
			return "", fmt.Errorf("could not resolve short name %q to a unique skill", identifier)
		}
		identifier = resolved
	}

	// Fetch meta and bundle
	var meta *SkillMeta
	var bundle *SkillBundle

	for _, src := range sources {
		if meta == nil {
			if m, err := src.Inspect(identifier); err == nil {
				meta = m
			}
		}
		if b, err := src.Fetch(identifier); err == nil {
			bundle = b
			if meta == nil {
				if m, err := src.Inspect(identifier); err == nil {
					meta = m
				}
			}
			break
		}
	}

	if bundle == nil {
		return "", fmt.Errorf("could not fetch skill %q from any source", identifier)
	}

	// Awaiting name resolution for url source
	if bundle.Source == "url" && (bundle.Name == "" || (bundle.Metadata != nil && bundle.Metadata["awaiting_name"] == true)) {
		if nameOverride != "" {
			bundle.Name = nameOverride
		} else if skipConfirm {
			return "", fmt.Errorf("cannot install from URL %q: SKILL.md lacks frontmatter name and no override provided via --name", identifier)
		} else {
			fmt.Printf("\nThe SKILL.md at %s doesn't declare a name in frontmatter.\nEnter a skill name: ", identifier)
			var answer string
			_, err := fmt.Scanln(&answer)
			if err != nil || strings.TrimSpace(answer) == "" {
				return "", fmt.Errorf("installation cancelled (invalid name)")
			}
			bundle.Name = strings.TrimSpace(answer)
		}
	}

	// Official category resolution: official/mlops/training/trl-fine-tuning -> mlops/training
	if bundle.Source == "official" && category == "" {
		parts := strings.Split(bundle.Identifier, "/")
		if len(parts) >= 3 {
			category = strings.Join(parts[1:len(parts)-1], "/")
		}
	}

	// Check if already installed
	lock := LoadLockFile()
	existing := lock.GetInstalled(bundle.Name)
	if existing != nil {
		if !force {
			return "", fmt.Errorf("skill %q is already installed at %s (use --force to reinstall)", bundle.Name, existing.InstallPath)
		}
	}

	// Quarantine
	qPath, err := quarantineBundle(bundle)
	if err != nil {
		appendAuditLog("BLOCKED", bundle.Name, bundle.Source, bundle.TrustLevel, "invalid_path", err.Error())
		return "", fmt.Errorf("quarantine failed: %v", err)
	}

	// Scan
	scanSource := bundle.Identifier
	if scanSource == "" {
		scanSource = identifier
	}
	scanResult := ScanSkill(qPath, scanSource)

	// Check scan verdict
	allowed, reason := shouldAllowInstall(scanResult, force)
	if !allowed {
		_ = os.RemoveAll(qPath)
		appendAuditLog("BLOCKED", bundle.Name, bundle.Source, bundle.TrustLevel, scanResult.Verdict, "blocked_by_guard")
		return "", fmt.Errorf("installation blocked: %s", reason)
	}

	// Confirm prompt if skipConfirm is false
	if !force && !skipConfirm {
		fmt.Printf("\nInstall skill %q? [y/N]: ", bundle.Name)
		var answer string
		_, _ = fmt.Scanln(&answer)
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			_ = os.RemoveAll(qPath)
			return "", fmt.Errorf("installation cancelled by user")
		}
	}

	// Install
	installDir, err := installFromQuarantine(qPath, bundle.Name, category, bundle, scanResult)
	if err != nil {
		_ = os.RemoveAll(qPath)
		appendAuditLog("BLOCKED", bundle.Name, bundle.Source, bundle.TrustLevel, "invalid_path", err.Error())
		return "", fmt.Errorf("install failed: %v", err)
	}

	// Invalidate snapshot cache
	invalidateCache()

	return installDir, nil
}

func InspectSkill(identifier string, auth *GitHubAuth) (*SkillMeta, *SkillBundle, error) {
	ensureHubDirs()
	var g GitHubAuth
	if auth != nil {
		g = *auth
	} else {
		g = ResolveGitHubAuth()
	}
	sources := CreateSourceRouter(&g)

	if !strings.Contains(identifier, "/") {
		resolved := ResolveShortName(identifier, sources)
		if resolved == "" {
			return nil, nil, fmt.Errorf("could not resolve short name %q", identifier)
		}
		identifier = resolved
	}

	var meta *SkillMeta
	var bundle *SkillBundle

	for _, src := range sources {
		if meta == nil {
			if m, err := src.Inspect(identifier); err == nil {
				meta = m
			}
		}
		if b, err := src.Fetch(identifier); err == nil {
			bundle = b
			if meta == nil {
				if m, err := src.Inspect(identifier); err == nil {
					meta = m
				}
			}
			break
		}
	}

	if meta == nil {
		return nil, nil, fmt.Errorf("could not find %q in any source", identifier)
	}

	return meta, bundle, nil
}

func UpdateSkills(name string, force bool, auth *GitHubAuth) ([]string, error) {
	var g GitHubAuth
	if auth != nil {
		g = *auth
	} else {
		g = ResolveGitHubAuth()
	}
	sources := CreateSourceRouter(&g)
	updates := CheckForSkillUpdates(name, sources)

	var updated []string
	for _, up := range updates {
		status, _ := up["status"].(string)
		if status == "update_available" || status == "unavailable" {
			n, _ := up["name"].(string)
			identifier, _ := up["identifier"].(string)

			// Resolve category
			lock := LoadLockFile()
			existing := lock.GetInstalled(n)
			category := ""
			if existing != nil {
				category = _deriveCategoryFromInstallPath(existing.InstallPath)
			}

			_, err := InstallSkill(identifier, category, n, true, true, &g)
			if err == nil {
				updated = append(updated, n)
			}
		}
	}
	return updated, nil
}

func _deriveCategoryFromInstallPath(installPath string) string {
	cleaned := filepath.Clean(installPath)
	dir := filepath.Dir(cleaned)
	if dir == "." || dir == "/" || dir == "" {
		return ""
	}
	return strings.ReplaceAll(dir, "\\", "/")
}

