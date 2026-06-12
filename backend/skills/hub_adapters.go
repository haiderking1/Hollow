package skills

import (
	"archive/zip"
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Helper: Quick Frontmatter Parse
// ---------------------------------------------------------------------------

func parseFrontmatterQuick(content string) map[string]interface{} {
	fm, _ := ParseFrontmatter(content)
	if fm != nil {
		return fm
	}

	// Fallback regex YAML parser
	if !strings.HasPrefix(content, "---") {
		return nil
	}
	idx := strings.Index(content[3:], "---")
	if idx == -1 {
		return nil
	}
	yamlText := content[3 : idx+3]
	var out map[string]interface{}
	if err := yaml.Unmarshal([]byte(yamlText), &out); err == nil {
		return out
	}
	return nil
}

// ---------------------------------------------------------------------------
// GitHub Source Adapter
// ---------------------------------------------------------------------------

type GitHubSource struct {
	auth        GitHubAuth
	taps        []TapEntry
	treeCache   map[string]githubTreeCacheEntry
	rateLimited bool
}

type githubTreeCacheEntry struct {
	branch  string
	entries []githubTreeEntry
}

type githubTreeEntry struct {
	Path string `json:"path"`
	Type string `json:"type"`
	SHA  string `json:"sha"`
}

var defaultTaps = []TapEntry{
	{Repo: "openai/skills", Path: "skills/.curated/"},
	{Repo: "openai/skills", Path: "skills/.system/"},
	{Repo: "anthropics/skills", Path: "skills/"},
	{Repo: "huggingface/skills", Path: "skills/"},
	{Repo: "NVIDIA/skills", Path: "skills/"},
	{Repo: "garrytan/gstack", Path: ""},
}

func (s *GitHubSource) SourceID() string {
	return "github"
}

func (s *GitHubSource) TrustLevelFor(identifier string) string {
	parts := strings.Split(identifier, "/")
	if len(parts) >= 2 {
		repo := parts[0] + "/" + parts[1]
		trusted := map[string]bool{
			"openai/skills":      true,
			"anthropics/skills":  true,
			"huggingface/skills": true,
			"nvidia/skills":      true,
		}
		if trusted[strings.ToLower(repo)] {
			return "trusted"
		}
	}
	return "community"
}

func (s *GitHubSource) Search(query string, limit int) []SkillMeta {
	s.init()
	queryLower := strings.ToLower(query)
	var results []SkillMeta

	// Load taps
	taps := append([]TapEntry(nil), defaultTaps...)
	taps = append(taps, LoadTaps()...)

	for _, tap := range taps {
		skills := s.listSkillsInRepo(tap.Repo, tap.Path)
		for _, sk := range skills {
			searchable := strings.ToLower(sk.Name + " " + sk.Description + " " + strings.Join(sk.Tags, " "))
			if strings.Contains(searchable, queryLower) {
				results = append(results, sk)
			}
		}
	}

	// Deduplicate
	seen := make(map[string]bool)
	var deduped []SkillMeta
	for _, r := range results {
		if !seen[r.Identifier] {
			seen[r.Identifier] = true
			deduped = append(deduped, r)
		}
	}

	if limit > 0 && len(deduped) > limit {
		deduped = deduped[:limit]
	}
	return deduped
}

func (s *GitHubSource) Fetch(identifier string) (*SkillBundle, error) {
	s.init()
	parts := strings.Split(identifier, "/")
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid github identifier: %s", identifier)
	}

	repo := parts[0] + "/" + parts[1]
	skillPath := strings.Join(parts[2:], "/")

	files, err := s.downloadDirectory(repo, skillPath)
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("failed to download: %v", err)
	}

	if _, ok := files["SKILL.md"]; !ok {
		return nil, fmt.Errorf("SKILL.md not found in download")
	}

	skillName := parts[len(parts)-1]
	trust := s.TrustLevelFor(identifier)

	return &SkillBundle{
		Name:       skillName,
		Files:      files,
		Source:     "github",
		Identifier: identifier,
		TrustLevel: trust,
	}, nil
}

func (s *GitHubSource) Inspect(identifier string) (*SkillMeta, error) {
	s.init()
	parts := strings.Split(identifier, "/")
	if len(parts) < 3 {
		return nil, fmt.Errorf("invalid github identifier: %s", identifier)
	}

	repo := parts[0] + "/" + parts[1]
	skillPath := strings.Join(parts[2:], "/")
	skillMdPath := skillPath + "/SKILL.md"

	content, err := s.fetchFileContent(repo, skillMdPath)
	if err != nil {
		return nil, err
	}

	fm := parseFrontmatterQuick(content)
	if fm == nil {
		fm = make(map[string]interface{})
	}

	name, _ := fm["name"].(string)
	if name == "" {
		name = parts[len(parts)-1]
	}
	desc := extractSkillDescription(fm)
	tags := extractSkillTags(fm)

	return &SkillMeta{
		Name:        name,
		Description: desc,
		Source:      "github",
		Identifier:  identifier,
		TrustLevel:  s.TrustLevelFor(identifier),
		Repo:        repo,
		Path:        skillPath,
		Tags:        tags,
	}, nil
}

func (s *GitHubSource) init() {
	if s.treeCache == nil {
		s.treeCache = make(map[string]githubTreeCacheEntry)
	}
}

func (s *GitHubSource) getRepoTree(repo string) (string, []githubTreeEntry, error) {
	if entry, ok := s.treeCache[repo]; ok {
		return entry.branch, entry.entries, nil
	}

	headers := s.auth.GetHeaders()

	// 1. Resolve default branch
	repoURL := fmt.Sprintf("https://api.github.com/repos/%s", repo)
	body, code, err := guardedHttpGet(repoURL, headers)
	if err != nil {
		return "", nil, err
	}
	if code == 403 || code == 429 {
		s.rateLimited = true
	}
	if code != 200 {
		return "", nil, fmt.Errorf("GitHub API returned status %d", code)
	}

	var repoInfo struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.Unmarshal(body, &repoInfo); err != nil {
		return "", nil, err
	}
	branch := repoInfo.DefaultBranch
	if branch == "" {
		branch = "main"
	}

	// 2. Fetch recursive tree
	treeURL := fmt.Sprintf("https://api.github.com/repos/%s/git/trees/%s?recursive=1", repo, branch)
	body, code, err = guardedHttpGet(treeURL, headers)
	if err != nil {
		return "", nil, err
	}
	if code != 200 {
		return "", nil, fmt.Errorf("GitHub Trees API returned status %d", code)
	}

	var treeData struct {
		Tree      []githubTreeEntry `json:"tree"`
		Truncated bool              `json:"truncated"`
	}
	if err := json.Unmarshal(body, &treeData); err != nil {
		return "", nil, err
	}

	s.treeCache[repo] = githubTreeCacheEntry{
		branch:  branch,
		entries: treeData.Tree,
	}

	return branch, treeData.Tree, nil
}

func (s *GitHubSource) listSkillsInRepo(repo string, rootPath string) []SkillMeta {
	cacheKey := fmt.Sprintf("gh_skills_%s_%s", strings.ReplaceAll(repo, "/", "_"), strings.ReplaceAll(rootPath, "/", "_"))
	if cached, ok := readIndexCache(cacheKey); ok {
		var out []SkillMeta
		if err := json.Unmarshal(cached, &out); err == nil {
			return out
		}
	}

	_, entries, err := s.getRepoTree(repo)
	if err != nil {
		return nil
	}

	prefix := rootPath
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	var skills []SkillMeta
	seenDirs := make(map[string]bool)

	for _, entry := range entries {
		if entry.Type != "blob" || !strings.HasSuffix(entry.Path, "/SKILL.md") {
			continue
		}
		if prefix != "" && !strings.HasPrefix(entry.Path, prefix) {
			continue
		}

		dir := filepath.Dir(entry.Path)
		if seenDirs[dir] {
			continue
		}
		seenDirs[dir] = true

		ident := repo + "/" + dir
		meta, err := s.Inspect(ident)
		if err == nil && meta != nil {
			skills = append(skills, *meta)
		}
	}

	if data, err := json.Marshal(skills); err == nil {
		writeIndexCache(cacheKey, data)
	}

	return skills
}

func (s *GitHubSource) downloadDirectory(repo string, skillPath string) (map[string][]byte, error) {
	_, entries, err := s.getRepoTree(repo)
	if err != nil {
		return nil, err
	}

	prefix := skillPath
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	files := make(map[string][]byte)
	for _, entry := range entries {
		if entry.Type != "blob" || !strings.HasPrefix(entry.Path, prefix) {
			continue
		}
		rel := strings.TrimPrefix(entry.Path, prefix)
		content, err := s.fetchFileContent(repo, entry.Path)
		if err == nil {
			files[rel] = []byte(content)
		}
	}
	return files, nil
}

func (s *GitHubSource) fetchFileContent(repo string, path string) (string, error) {
	urlStr := fmt.Sprintf("https://api.github.com/repos/%s/contents/%s", repo, path)
	headers := s.auth.GetHeaders()
	headers["Accept"] = "application/vnd.github.v3.raw"

	body, code, err := guardedHttpGet(urlStr, headers)
	if err != nil {
		return "", err
	}
	if code != 200 {
		return "", fmt.Errorf("contents API returned status %d", code)
	}
	return string(body), nil
}

// ---------------------------------------------------------------------------
// URL Source Adapter
// ---------------------------------------------------------------------------

type UrlSource struct{}

func (s *UrlSource) SourceID() string {
	return "url"
}

func (s *UrlSource) TrustLevelFor(identifier string) string {
	return "community"
}

func (s *UrlSource) Search(query string, limit int) []SkillMeta {
	return nil
}

func (s *UrlSource) matches(identifier string) bool {
	ident := strings.TrimSpace(identifier)
	lower := strings.ToLower(ident)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return false
	}
	if strings.Contains(lower, "/.well-known/skills/") || strings.HasSuffix(lower, "/index.json") {
		return false
	}
	u, err := url.Parse(ident)
	if err != nil {
		return false
	}
	return strings.HasSuffix(strings.ToLower(u.Path), ".md")
}

func (s *UrlSource) Inspect(identifier string) (*SkillMeta, error) {
	if !s.matches(identifier) {
		return nil, fmt.Errorf("not a direct url skill")
	}

	body, code, err := guardedHttpGet(identifier, nil)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("failed to fetch url, status: %d", code)
	}

	fm := parseFrontmatterQuick(string(body))
	if fm == nil {
		fm = make(map[string]interface{})
	}

	name, _ := fm["name"].(string)
	if name == "" {
		name = s.resolveSkillName(identifier)
	}

	desc := extractSkillDescription(fm)
	tags := extractSkillTags(fm)

	return &SkillMeta{
		Name:        name,
		Description: desc,
		Source:      "url",
		Identifier:  identifier,
		TrustLevel:  "community",
		Tags:        tags,
	}, nil
}

func (s *UrlSource) Fetch(identifier string) (*SkillBundle, error) {
	if !s.matches(identifier) {
		return nil, fmt.Errorf("not a direct url skill")
	}

	body, code, err := guardedHttpGet(identifier, nil)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("failed to fetch url, status: %d", code)
	}

	fm := parseFrontmatterQuick(string(body))
	name := ""
	if fm != nil {
		name, _ = fm["name"].(string)
	}
	if name == "" {
		name = s.resolveSkillName(identifier)
	}

	files := map[string][]byte{
		"SKILL.md": body,
	}

	return &SkillBundle{
		Name:       name,
		Files:      files,
		Source:     "url",
		Identifier: identifier,
		TrustLevel: "community",
	}, nil
}

func (s *UrlSource) resolveSkillName(urlStr string) string {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "unnamed-skill"
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 0 {
		return "unnamed-skill"
	}
	last := parts[len(parts)-1]
	if strings.ToLower(last) == "skill.md" && len(parts) >= 2 {
		last = parts[len(parts)-2]
	}
	last = strings.TrimSuffix(last, ".md")
	last = strings.TrimSuffix(last, ".MD")
	return strings.ToLower(last)
}

// ---------------------------------------------------------------------------
// Well-Known Agent Skills Source Adapter
// ---------------------------------------------------------------------------

type WellKnownSkillSource struct{}

func (s *WellKnownSkillSource) SourceID() string {
	return "well-known"
}

func (s *WellKnownSkillSource) TrustLevelFor(identifier string) string {
	return "community"
}

type wellKnownIndex struct {
	Skills []wellKnownEntry `json:"skills"`
}

type wellKnownEntry struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Files       []string `json:"files"`
}

func (s *WellKnownSkillSource) parseIdentifier(identifier string) (indexURL, baseURL, skillName, skillURL string, ok bool) {
	raw := identifier
	if strings.HasPrefix(raw, "well-known:") {
		raw = raw[len("well-known:"):]
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return "", "", "", "", false
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", "", "", "", false
	}

	cleanURL := u.Scheme + "://" + u.Host + u.Path
	fragment := u.Fragment

	if strings.HasSuffix(cleanURL, "/index.json") {
		if fragment == "" {
			return "", "", "", "", false
		}
		baseURL = cleanURL[:len(cleanURL)-len("/index.json")]
		return cleanURL, baseURL, fragment, baseURL + "/" + fragment, true
	}

	if strings.HasSuffix(cleanURL, "/SKILL.md") {
		skillURL = cleanURL[:len(cleanURL)-len("/SKILL.md")]
	} else {
		skillURL = strings.TrimSuffix(cleanURL, "/")
	}

	if !strings.Contains(skillURL, "/.well-known/skills/") {
		return "", "", "", "", false
	}

	idx := strings.LastIndex(skillURL, "/")
	if idx == -1 {
		return "", "", "", "", false
	}
	baseURL = skillURL[:idx]
	skillName = skillURL[idx+1:]
	indexURL = baseURL + "/index.json"

	return indexURL, baseURL, skillName, skillURL, true
}

func (s *WellKnownSkillSource) fetchIndex(indexURL string) (*wellKnownIndex, error) {
	cacheKey := "well_known_index_" + md5Hash(indexURL)
	if cached, ok := readIndexCache(cacheKey); ok {
		var idx wellKnownIndex
		if err := json.Unmarshal(cached, &idx); err == nil {
			return &idx, nil
		}
	}

	body, code, err := guardedHttpGet(indexURL, nil)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("failed to fetch well-known index: %d", code)
	}

	var idx wellKnownIndex
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, err
	}

	writeIndexCache(cacheKey, body)
	return &idx, nil
}

func (s *WellKnownSkillSource) Search(query string, limit int) []SkillMeta {
	query = strings.TrimSpace(query)
	indexURL := ""
	if strings.HasPrefix(query, "http://") || strings.HasPrefix(query, "https://") {
		if strings.HasSuffix(query, "/index.json") {
			indexURL = query
		} else if strings.Contains(query, "/.well-known/skills/") {
			indexURL = strings.Split(query, "/.well-known/skills/")[0] + "/.well-known/skills/index.json"
		} else {
			indexURL = strings.TrimSuffix(query, "/") + "/.well-known/skills/index.json"
		}
	} else {
		return nil
	}

	idx, err := s.fetchIndex(indexURL)
	if err != nil {
		return nil
	}

	baseURL := indexURL[:len(indexURL)-len("/index.json")]

	var results []SkillMeta
	for _, entry := range idx.Skills {
		files := entry.Files
		if len(files) == 0 {
			files = []string{"SKILL.md"}
		}
		results = append(results, SkillMeta{
			Name:        entry.Name,
			Description: entry.Description,
			Source:      "well-known",
			Identifier:  "well-known:" + baseURL + "/" + entry.Name,
			TrustLevel:  "community",
			Path:        entry.Name,
			Extra: map[string]interface{}{
				"index_url": indexURL,
				"base_url":  baseURL,
				"files":     files,
			},
		})
	}

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results
}

func (s *WellKnownSkillSource) Inspect(identifier string) (*SkillMeta, error) {
	indexURL, baseURL, skillName, skillURL, ok := s.parseIdentifier(identifier)
	if !ok {
		return nil, fmt.Errorf("invalid well-known identifier")
	}

	idx, err := s.fetchIndex(indexURL)
	if err != nil {
		return nil, err
	}

	var match *wellKnownEntry
	for _, entry := range idx.Skills {
		if entry.Name == skillName {
			match = &entry
			break
		}
	}
	if match == nil {
		return nil, fmt.Errorf("skill not found in index")
	}

	skillMdURL := skillURL + "/SKILL.md"
	body, code, err := guardedHttpGet(skillMdURL, nil)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("failed to fetch SKILL.md")
	}

	fm := parseFrontmatterQuick(string(body))
	desc := match.Description
	if fm != nil {
		if d := extractSkillDescription(fm); d != "" {
			desc = d
		}
	}

	files := match.Files
	if len(files) == 0 {
		files = []string{"SKILL.md"}
	}

	return &SkillMeta{
		Name:        skillName,
		Description: desc,
		Source:      "well-known",
		Identifier:  identifier,
		TrustLevel:  "community",
		Path:        skillName,
		Extra: map[string]interface{}{
			"index_url": indexURL,
			"base_url":  baseURL,
			"files":     files,
			"endpoint":  skillURL,
		},
	}, nil
}

func (s *WellKnownSkillSource) Fetch(identifier string) (*SkillBundle, error) {
	meta, err := s.Inspect(identifier)
	if err != nil {
		return nil, err
	}

	extra := meta.Extra
	endpoint, _ := extra["endpoint"].(string)
	fileList, _ := extra["files"].([]string)

	files := make(map[string][]byte)
	for _, relPath := range fileList {
		safeRel, err := normalizeBundlePath(relPath, "well-known file path", true)
		if err != nil {
			return nil, err
		}
		fileURL := endpoint + "/" + safeRel
		body, code, err := guardedHttpGet(fileURL, nil)
		if err != nil || code != 200 {
			return nil, fmt.Errorf("failed to fetch %s: %v", relPath, err)
		}
		files[safeRel] = body
	}

	return &SkillBundle{
		Name:       meta.Name,
		Files:      files,
		Source:     "well-known",
		Identifier: identifier,
		TrustLevel: "community",
		Metadata:   extra,
	}, nil
}

// ---------------------------------------------------------------------------
// Skills.sh Source Adapter
// ---------------------------------------------------------------------------

type SkillsShSource struct {
	auth   GitHubAuth
	github GitHubSource
}

func (s *SkillsShSource) SourceID() string {
	return "skills-sh"
}

func (s *SkillsShSource) TrustLevelFor(identifier string) string {
	s.github.auth = s.auth
	return s.github.TrustLevelFor(s.normalizeIdentifier(identifier))
}

func (s *SkillsShSource) normalizeIdentifier(identifier string) string {
	raw := identifier
	prefixes := []string{"skills-sh/", "skills.sh/", "skils-sh/", "skils.sh/"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(raw, prefix) {
			return raw[len(prefix):]
		}
	}
	return raw
}

func (s *SkillsShSource) Search(query string, limit int) []SkillMeta {
	s.github.auth = s.auth
	query = strings.TrimSpace(query)
	if query == "" {
		return s.sitemapCatalog(limit)
	}

	cacheKey := "skills_sh_search_" + md5Hash(fmt.Sprintf("%s|%d", query, limit))
	if cached, ok := readIndexCache(cacheKey); ok {
		var out []SkillMeta
		if err := json.Unmarshal(cached, &out); err == nil {
			return out
		}
	}

	searchURL := fmt.Sprintf("https://skills.sh/api/search?q=%s&limit=%d", url.QueryEscape(query), limit)
	body, code, err := guardedHttpGet(searchURL, nil)
	if err != nil || code != 200 {
		return nil
	}

	var data struct {
		Skills []struct {
			ID          string `json:"id"`
			Name        string `json:"name"`
			Source      string `json:"source"`
			SkillID     string `json:"skillId"`
			Description string `json:"description"`
			Installs    int    `json:"installs"`
		} `json:"skills"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil
	}

	var results []SkillMeta
	for _, item := range data.Skills {
		canonical := item.ID
		if canonical == "" {
			canonical = item.Source + "/" + item.SkillID
		}
		parts := strings.Split(canonical, "/")
		if len(parts) < 3 {
			continue
		}
		repo := parts[0] + "/" + parts[1]
		skillPath := parts[2]

		installsLabel := ""
		if item.Installs > 0 {
			installsLabel = fmt.Sprintf(" · %d installs", item.Installs)
		}

		results = append(results, SkillMeta{
			Name:        item.Name,
			Description: fmt.Sprintf("Indexed by skills.sh from %s%s", repo, installsLabel),
			Source:      "skills.sh",
			Identifier:  "skills-sh/" + canonical,
			TrustLevel:  s.TrustLevelFor(canonical),
			Repo:        repo,
			Path:        skillPath,
			Extra: map[string]interface{}{
				"installs":   item.Installs,
				"detail_url": "https://skills.sh/" + canonical,
				"repo_url":   "https://github.com/" + repo,
			},
		})
	}

	if dataBytes, err := json.Marshal(results); err == nil {
		writeIndexCache(cacheKey, dataBytes)
	}

	return results
}

func (s *SkillsShSource) sitemapCatalog(limit int) []SkillMeta {
	cacheKey := "skills_sh_sitemap_v1"
	if cached, ok := readIndexCache(cacheKey); ok {
		var out []SkillMeta
		if err := json.Unmarshal(cached, &out); err == nil {
			if limit > 0 && len(out) > limit {
				return out[:limit]
			}
			return out
		}
	}

	// Fetch sitemap
	sitemapURL := "https://www.skills.sh/sitemap.xml"
	body, code, err := guardedHttpGet(sitemapURL, map[string]string{"Accept-Encoding": "gzip"})
	if err != nil || code != 200 {
		return nil
	}

	locRe := regexp.MustCompile(`(?i)<loc>([^<]+)</loc>`)
	matches := locRe.FindAllStringSubmatch(string(body), -1)

	var sitemaps []string
	for _, m := range matches {
		loc := strings.TrimSpace(m[1])
		if strings.Contains(loc, "sitemap-skills") {
			sitemaps = append(sitemaps, loc)
		}
	}

	var results []SkillMeta
	seen := make(map[string]bool)

	skillRe := regexp.MustCompile(`(?i)^https?://(?:www\.)?skills\.sh/([^/]+)/([^/]+)/([^/]+)/?$`)

	for _, smURL := range sitemaps {
		smBody, code, err := guardedHttpGet(smURL, map[string]string{"Accept-Encoding": "gzip"})
		if err != nil || code != 200 {
			continue
		}
		smMatches := locRe.FindAllStringSubmatch(string(smBody), -1)
		for _, m := range smMatches {
			u := strings.TrimSpace(m[1])
			smSub := skillRe.FindStringSubmatch(u)
			if len(smSub) < 4 {
				continue
			}
			owner := smSub[1]
			repoName := smSub[2]
			skillName := smSub[3]
			canonical := owner + "/" + repoName + "/" + skillName

			if seen[canonical] {
				continue
			}
			seen[canonical] = true
			repo := owner + "/" + repoName

			results = append(results, SkillMeta{
				Name:        skillName,
				Description: "Indexed by skills.sh from " + repo,
				Source:      "skills.sh",
				Identifier:  "skills-sh/" + canonical,
				TrustLevel:  s.TrustLevelFor(canonical),
				Repo:        repo,
				Path:        skillName,
				Extra: map[string]interface{}{
					"detail_url": "https://skills.sh/" + canonical,
					"repo_url":   "https://github.com/" + repo,
				},
			})
		}
	}

	if len(results) > 0 {
		if dataBytes, err := json.Marshal(results); err == nil {
			writeIndexCache(cacheKey, dataBytes)
		}
	}

	if limit > 0 && len(results) > limit {
		return results[:limit]
	}
	return results
}

func (s *SkillsShSource) Fetch(identifier string) (*SkillBundle, error) {
	s.github.auth = s.auth
	canonical := s.normalizeIdentifier(identifier)

	// Fetch detail page to discover actual repo + path
	detail := s.fetchDetailPage(canonical)
	repo := canonical[:strings.LastIndex(canonical, "/")]
	if detail != nil && detail["repo"] != nil {
		repo, _ = detail["repo"].(string)
	}

	// Try candidates
	candidates := []string{
		canonical,
		repo + "/skills/" + canonical[strings.LastIndex(canonical, "/")+1:],
		repo + "/.agents/skills/" + canonical[strings.LastIndex(canonical, "/")+1:],
		repo + "/.claude/skills/" + canonical[strings.LastIndex(canonical, "/")+1:],
	}

	for _, cand := range candidates {
		bundle, err := s.github.Fetch(cand)
		if err == nil && bundle != nil {
			bundle.Source = "skills.sh"
			bundle.Identifier = "skills-sh/" + canonical
			if detail != nil {
				bundle.Metadata = detail
			}
			return bundle, nil
		}
	}

	return nil, fmt.Errorf("failed to fetch skills-sh bundle: %s", identifier)
}

func (s *SkillsShSource) Inspect(identifier string) (*SkillMeta, error) {
	s.github.auth = s.auth
	canonical := s.normalizeIdentifier(identifier)
	detail := s.fetchDetailPage(canonical)

	repo := canonical[:strings.LastIndex(canonical, "/")]
	if detail != nil && detail["repo"] != nil {
		repo, _ = detail["repo"].(string)
	}

	candidates := []string{
		canonical,
		repo + "/skills/" + canonical[strings.LastIndex(canonical, "/")+1:],
		repo + "/.agents/skills/" + canonical[strings.LastIndex(canonical, "/")+1:],
		repo + "/.claude/skills/" + canonical[strings.LastIndex(canonical, "/")+1:],
	}

	var meta *SkillMeta
	var err error
	for _, cand := range candidates {
		meta, err = s.github.Inspect(cand)
		if err == nil && meta != nil {
			break
		}
	}

	if meta == nil {
		return nil, fmt.Errorf("failed to inspect skills-sh: %s", identifier)
	}

	meta.Source = "skills.sh"
	meta.Identifier = "skills-sh/" + canonical
	meta.TrustLevel = s.TrustLevelFor(canonical)
	if detail != nil {
		meta.Extra = detail
		if summary, ok := detail["body_summary"].(string); ok && summary != "" {
			meta.Description = summary
		}
	}

	return meta, nil
}

func (s *SkillsShSource) fetchDetailPage(identifier string) map[string]interface{} {
	cacheKey := "skills_sh_detail_" + md5Hash(identifier)
	if cached, ok := readIndexCache(cacheKey); ok {
		var out map[string]interface{}
		if err := json.Unmarshal(cached, &out); err == nil {
			return out
		}
	}

	detailURL := "https://skills.sh/" + identifier
	body, code, err := guardedHttpGet(detailURL, nil)
	if err != nil || code != 200 {
		return nil
	}

	html := string(body)
	detail := make(map[string]interface{})

	// parse title
	titleRe := regexp.MustCompile(`(?i)<h1[^>]*>(.*?)</h1>`)
	titleMatch := titleRe.FindStringSubmatch(html)
	if len(titleMatch) > 1 {
		detail["page_title"] = stripHTML(titleMatch[1])
	}

	// parse prose summary
	proseRe := regexp.MustCompile(`(?i)<div[^>]*class=["\'][^"\']*prose[^"\']*["\'][^>]*>.*?<p[^>]*>(.*?)</p>`)
	proseMatch := proseRe.FindStringSubmatch(html)
	if len(proseMatch) > 1 {
		detail["body_summary"] = stripHTML(proseMatch[1])
	}

	// parse install count
	installsRe := regexp.MustCompile(`(?i)Weekly Installs.*?children\\":\\"(#[0-9.,Kk]+|[0-9.,Kk]+)\\"`)
	installsMatch := installsRe.FindStringSubmatch(html)
	if len(installsMatch) > 1 {
		detail["weekly_installs"] = installsMatch[1]
	}

	// extract repo
	installCmdRe := regexp.MustCompile(`(?i)npx\s+skills\s+add\s+(https?://github\.com/[^\s<]+|[^\s<]+)`)
	installMatch := installCmdRe.FindStringSubmatch(html)
	if len(installMatch) > 1 {
		repoVal := strings.TrimSpace(installMatch[1])
		if strings.HasPrefix(repoVal, "https://github.com/") {
			repoVal = strings.TrimPrefix(repoVal, "https://github.com/")
		}
		repoVal = strings.Trim(repoVal, "/")
		parts := strings.Split(repoVal, "/")
		if len(parts) >= 2 {
			detail["repo"] = parts[0] + "/" + parts[1]
		}
	}

	detail["detail_url"] = detailURL

	if dataBytes, err := json.Marshal(detail); err == nil {
		writeIndexCache(cacheKey, dataBytes)
	}

	return detail
}

func stripHTML(val string) string {
	re := regexp.MustCompile(`<[^>]+>`)
	return strings.TrimSpace(re.ReplaceAllString(val, ""))
}

// ---------------------------------------------------------------------------
// ClawHub Source Adapter
// ---------------------------------------------------------------------------

type ClawHubSource struct{}

func (s *ClawHubSource) SourceID() string {
	return "clawhub"
}

func (s *ClawHubSource) TrustLevelFor(identifier string) string {
	return "community"
}

func (s *ClawHubSource) Search(query string, limit int) []SkillMeta {
	query = strings.TrimSpace(query)
	cacheKey := "clawhub_search_" + md5Hash(fmt.Sprintf("%s|%d", query, limit))
	if cached, ok := readIndexCache(cacheKey); ok {
		var out []SkillMeta
		if err := json.Unmarshal(cached, &out); err == nil {
			return out
		}
	}

	searchURL := fmt.Sprintf("https://clawhub.ai/api/v1/skills?search=%s&limit=%d", url.QueryEscape(query), limit)
	body, code, err := guardedHttpGet(searchURL, nil)
	if err != nil || code != 200 {
		return nil
	}

	var data struct {
		Items []struct {
			Slug        string   `json:"slug"`
			Name        string   `json:"name"`
			DisplayName string   `json:"displayName"`
			Summary     string   `json:"summary"`
			Description string   `json:"description"`
			Tags        []string `json:"tags"`
		} `json:"items"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		// Try unmarshaling direct array fallback
		var itemsDirect []struct {
			Slug        string   `json:"slug"`
			Name        string   `json:"name"`
			DisplayName string   `json:"displayName"`
			Summary     string   `json:"summary"`
			Description string   `json:"description"`
			Tags        []string `json:"tags"`
		}
		if err := json.Unmarshal(body, &itemsDirect); err == nil {
			data.Items = itemsDirect
		} else {
			return nil
		}
	}

	var results []SkillMeta
	for _, item := range data.Items {
		name := item.DisplayName
		if name == "" {
			name = item.Name
		}
		if name == "" {
			name = item.Slug
		}
		desc := item.Summary
		if desc == "" {
			desc = item.Description
		}

		results = append(results, SkillMeta{
			Name:        name,
			Description: desc,
			Source:      "clawhub",
			Identifier:  item.Slug,
			TrustLevel:  "community",
			Tags:        item.Tags,
		})
	}

	if dataBytes, err := json.Marshal(results); err == nil {
		writeIndexCache(cacheKey, dataBytes)
	}

	return results
}

func (s *ClawHubSource) Inspect(identifier string) (*SkillMeta, error) {
	slug := identifier
	if idx := strings.LastIndex(slug, "/"); idx != -1 {
		slug = slug[idx+1:]
	}

	detailURL := fmt.Sprintf("https://clawhub.ai/api/v1/skills/%s", slug)
	body, code, err := guardedHttpGet(detailURL, nil)
	if err != nil || code != 200 {
		return nil, fmt.Errorf("failed to inspect clawhub: %s", identifier)
	}

	var data struct {
		Slug        string   `json:"slug"`
		Name        string   `json:"name"`
		DisplayName string   `json:"displayName"`
		Summary     string   `json:"summary"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
	}
	// Support nested "skill" structure
	var nested struct {
		Skill struct {
			Slug        string   `json:"slug"`
			Name        string   `json:"name"`
			DisplayName string   `json:"displayName"`
			Summary     string   `json:"summary"`
			Description string   `json:"description"`
			Tags        []string `json:"tags"`
		} `json:"skill"`
	}

	if err := json.Unmarshal(body, &nested); err == nil && nested.Skill.Slug != "" {
		data = nested.Skill
	} else if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	name := data.DisplayName
	if name == "" {
		name = data.Name
	}
	if name == "" {
		name = data.Slug
	}
	desc := data.Summary
	if desc == "" {
		desc = data.Description
	}

	return &SkillMeta{
		Name:        name,
		Description: desc,
		Source:      "clawhub",
		Identifier:  data.Slug,
		TrustLevel:  "community",
		Tags:        data.Tags,
	}, nil
}

func (s *ClawHubSource) Fetch(identifier string) (*SkillBundle, error) {
	slug := identifier
	if idx := strings.LastIndex(slug, "/"); idx != -1 {
		slug = slug[idx+1:]
	}

	detailURL := fmt.Sprintf("https://clawhub.ai/api/v1/skills/%s", slug)
	body, code, err := guardedHttpGet(detailURL, nil)
	if err != nil || code != 200 {
		return nil, fmt.Errorf("failed to inspect clawhub: %s", identifier)
	}

	var data struct {
		Slug          string `json:"slug"`
		LatestVersion string `json:"latestVersion"`
	}
	var nested struct {
		Skill struct {
			Slug          string `json:"slug"`
			LatestVersion string `json:"latestVersion"`
		} `json:"skill"`
		LatestVersion string `json:"latestVersion"`
	}

	v := ""
	if err := json.Unmarshal(body, &nested); err == nil {
		if nested.Skill.LatestVersion != "" {
			v = nested.Skill.LatestVersion
		} else {
			v = nested.LatestVersion
		}
	} else if err := json.Unmarshal(body, &data); err == nil {
		v = data.LatestVersion
	}

	if v == "" {
		// fetch versions list
		versionsURL := fmt.Sprintf("https://clawhub.ai/api/v1/skills/%s/versions", slug)
		vBody, vCode, err := guardedHttpGet(versionsURL, nil)
		if err == nil && vCode == 200 {
			var vList []struct {
				Version string `json:"version"`
			}
			if err := json.Unmarshal(vBody, &vList); err == nil && len(vList) > 0 {
				v = vList[0].Version
			}
		}
	}

	if v == "" {
		return nil, fmt.Errorf("failed to resolve latest version")
	}

	// Download ZIP
	dlURL := fmt.Sprintf("https://clawhub.ai/api/v1/download?slug=%s&version=%s", slug, v)
	zipBody, dlCode, err := guardedHttpGet(dlURL, nil)
	if err != nil || dlCode != 200 {
		return nil, fmt.Errorf("failed to download ZIP, status: %d", dlCode)
	}

	files := make(map[string][]byte)
	r, err := zip.NewReader(bytes.NewReader(zipBody), int64(len(zipBody)))
	if err != nil {
		return nil, err
	}

	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		safeRel, err := normalizeBundlePath(f.Name, "zip member path", true)
		if err != nil {
			continue
		}
		if f.FileInfo().Size() > 500000 { // limit size to 500KB
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err == nil {
			files[safeRel] = content
		}
	}

	if _, ok := files["SKILL.md"]; !ok {
		// Fallback version metadata endpoint
		vURL := fmt.Sprintf("https://clawhub.ai/api/v1/skills/%s/versions/%s", slug, v)
		vBody, vCode, err := guardedHttpGet(vURL, nil)
		if err == nil && vCode == 200 {
			var vData struct {
				Files map[string]string `json:"files"`
			}
			var vNested struct {
				Version struct {
					Files map[string]string `json:"files"`
				} `json:"version"`
			}
			fMap := make(map[string]string)
			if err := json.Unmarshal(vBody, &vNested); err == nil && len(vNested.Version.Files) > 0 {
				fMap = vNested.Version.Files
			} else if err := json.Unmarshal(vBody, &vData); err == nil {
				fMap = vData.Files
			}
			for fPath, fText := range fMap {
				safeRel, err := normalizeBundlePath(fPath, "version metadata path", true)
				if err == nil {
					files[safeRel] = []byte(fText)
				}
			}
		}
	}

	if _, ok := files["SKILL.md"]; !ok {
		return nil, fmt.Errorf("failed to extract SKILL.md")
	}

	return &SkillBundle{
		Name:       slug,
		Files:      files,
		Source:     "clawhub",
		Identifier: identifier,
		TrustLevel: "community",
	}, nil
}

// ---------------------------------------------------------------------------
// Claude Code Marketplace Source Adapter
// ---------------------------------------------------------------------------

type ClaudeMarketplaceSource struct {
	auth   GitHubAuth
	github GitHubSource
}

func (s *ClaudeMarketplaceSource) SourceID() string {
	return "claude-marketplace"
}

func (s *ClaudeMarketplaceSource) TrustLevelFor(identifier string) string {
	s.github.auth = s.auth
	return s.github.TrustLevelFor(identifier)
}

func (s *ClaudeMarketplaceSource) Search(query string, limit int) []SkillMeta {
	return nil // Skip search for now or delegate
}

func (s *ClaudeMarketplaceSource) Inspect(identifier string) (*SkillMeta, error) {
	s.github.auth = s.auth
	meta, err := s.github.Inspect(identifier)
	if err == nil && meta != nil {
		meta.Source = "claude-marketplace"
		meta.TrustLevel = s.TrustLevelFor(identifier)
	}
	return meta, err
}

func (s *ClaudeMarketplaceSource) Fetch(identifier string) (*SkillBundle, error) {
	s.github.auth = s.auth
	bundle, err := s.github.Fetch(identifier)
	if err == nil && bundle != nil {
		bundle.Source = "claude-marketplace"
	}
	return bundle, err
}

// ---------------------------------------------------------------------------
// LobeHub Source Adapter
// ---------------------------------------------------------------------------

type LobeHubSource struct{}

func (s *LobeHubSource) SourceID() string {
	return "lobehub"
}

func (s *LobeHubSource) TrustLevelFor(identifier string) string {
	return "community"
}

func (s *LobeHubSource) Search(query string, limit int) []SkillMeta {
	queryLower := strings.ToLower(query)
	index, err := s.fetchIndex()
	if err != nil {
		return nil
	}

	var results []SkillMeta
	for _, agent := range index {
		title := agent.Meta.Title
		if title == "" {
			title = agent.Identifier
		}
		searchable := strings.ToLower(title + " " + agent.Meta.Description + " " + strings.Join(agent.Meta.Tags, " "))
		if strings.Contains(searchable, queryLower) {
			results = append(results, SkillMeta{
				Name:        agent.Identifier,
				Description: agent.Meta.Description,
				Source:      "lobehub",
				Identifier:  "lobehub/" + agent.Identifier,
				TrustLevel:  "community",
				Tags:        agent.Meta.Tags,
			})
			if len(results) >= limit {
				break
			}
		}
	}
	return results
}

type lobehubIndexAgent struct {
	Identifier string `json:"identifier"`
	Meta       struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
	} `json:"meta"`
}

func (s *LobeHubSource) fetchIndex() ([]lobehubIndexAgent, error) {
	cacheKey := "lobehub_index"
	if cached, ok := readIndexCache(cacheKey); ok {
		var idx []lobehubIndexAgent
		if err := json.Unmarshal(cached, &idx); err == nil {
			return idx, nil
		}
		var wrapper struct {
			Agents []lobehubIndexAgent `json:"agents"`
		}
		if err := json.Unmarshal(cached, &wrapper); err == nil {
			return wrapper.Agents, nil
		}
	}

	body, code, err := guardedHttpGet("https://chat-agents.lobehub.com/index.json", nil)
	if err != nil || code != 200 {
		return nil, fmt.Errorf("failed to fetch lobehub index: %v", err)
	}

	var idx []lobehubIndexAgent
	var wrapper struct {
		Agents []lobehubIndexAgent `json:"agents"`
	}
	if err := json.Unmarshal(body, &wrapper); err == nil && len(wrapper.Agents) > 0 {
		idx = wrapper.Agents
	} else if err := json.Unmarshal(body, &idx); err != nil {
		return nil, err
	}

	writeIndexCache(cacheKey, body)
	return idx, nil
}

func (s *LobeHubSource) Inspect(identifier string) (*SkillMeta, error) {
	agentID := identifier
	if strings.HasPrefix(agentID, "lobehub/") {
		agentID = agentID[len("lobehub/"):]
	}

	idx, err := s.fetchIndex()
	if err != nil {
		return nil, err
	}

	for _, agent := range idx {
		if agent.Identifier == agentID {
			return &SkillMeta{
				Name:        agentID,
				Description: agent.Meta.Description,
				Source:      "lobehub",
				Identifier:  identifier,
				TrustLevel:  "community",
				Tags:        agent.Meta.Tags,
			}, nil
		}
	}

	return nil, fmt.Errorf("lobehub agent not found: %s", agentID)
}

type lobehubAgentData struct {
	Identifier string `json:"identifier"`
	Meta       struct {
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
	} `json:"meta"`
	Config struct {
		SystemRole string `json:"systemRole"`
	} `json:"config"`
}

func (s *LobeHubSource) Fetch(identifier string) (*SkillBundle, error) {
	agentID := identifier
	if strings.HasPrefix(agentID, "lobehub/") {
		agentID = agentID[len("lobehub/"):]
	}

	urlStr := fmt.Sprintf("https://chat-agents.lobehub.com/%s.json", agentID)
	body, code, err := guardedHttpGet(urlStr, nil)
	if err != nil || code != 200 {
		return nil, fmt.Errorf("failed to fetch lobehub agent config: %v", err)
	}

	var data lobehubAgentData
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	// convert to SKILL.md
	title := data.Meta.Title
	if title == "" {
		title = agentID
	}
	tagsStr := ""
	if len(data.Meta.Tags) > 0 {
		tagsStr = fmt.Sprintf("    tags: [%s]", strings.Join(data.Meta.Tags, ", "))
	}

	fmText := fmt.Sprintf(`---
name: %s
description: %s
metadata:
  hermes:
%s
  lobehub:
    source: lobehub
---

# %s

%s

## Instructions

%s
`, data.Identifier, strings.ReplaceAll(data.Meta.Description, "\n", " "), tagsStr, title, data.Meta.Description, data.Config.SystemRole)

	files := map[string][]byte{
		"SKILL.md": []byte(fmText),
	}

	return &SkillBundle{
		Name:       agentID,
		Files:      files,
		Source:     "lobehub",
		Identifier: identifier,
		TrustLevel: "community",
	}, nil
}

// ---------------------------------------------------------------------------
// Browse.sh Source Adapter
// ---------------------------------------------------------------------------

type BrowseShSource struct{}

func (s *BrowseShSource) SourceID() string {
	return "browse-sh"
}

func (s *BrowseShSource) TrustLevelFor(identifier string) string {
	return "community"
}

type browseShItem struct {
	Slug              string   `json:"slug"`
	Name              string   `json:"name"`
	Title             string   `json:"title"`
	Description       string   `json:"description"`
	Hostname          string   `json:"hostname"`
	Category          string   `json:"category"`
	Tags              []string `json:"tags"`
	SourceURL         string   `json:"sourceUrl"`
	RecommendedMethod string   `json:"recommendedMethod"`
	Proxies           bool     `json:"proxies"`
	InstallCount      int      `json:"installCount"`
}

func (s *BrowseShSource) fetchCatalog() ([]browseShItem, error) {
	cacheKey := "browse_sh_catalog"
	if cached, ok := readIndexCache(cacheKey); ok {
		var catalog []browseShItem
		if err := json.Unmarshal(cached, &catalog); err == nil {
			return catalog, nil
		}
		var wrapper struct {
			Skills []browseShItem `json:"skills"`
		}
		if err := json.Unmarshal(cached, &wrapper); err == nil {
			return wrapper.Skills, nil
		}
	}

	body, code, err := guardedHttpGet("https://browse.sh/api/skills", nil)
	if err != nil || code != 200 {
		return nil, fmt.Errorf("failed to fetch browse.sh catalog: %v", err)
	}

	var catalog []browseShItem
	var wrapper struct {
		Skills []browseShItem `json:"skills"`
	}
	if err := json.Unmarshal(body, &wrapper); err == nil && len(wrapper.Skills) > 0 {
		catalog = wrapper.Skills
	} else if err := json.Unmarshal(body, &catalog); err != nil {
		return nil, err
	}

	writeIndexCache(cacheKey, body)
	return catalog, nil
}

func (s *BrowseShSource) Search(query string, limit int) []SkillMeta {
	catalog, err := s.fetchCatalog()
	if err != nil {
		return nil
	}

	queryLower := strings.ToLower(query)
	var results []SkillMeta
	for _, item := range catalog {
		text := strings.ToLower(item.Name + " " + item.Title + " " + item.Description + " " + item.Hostname + " " + item.Category + " " + strings.Join(item.Tags, " "))
		if queryLower == "" || strings.Contains(text, queryLower) {
			desc := item.Description
			if desc == "" {
				desc = item.Title
			}
			results = append(results, SkillMeta{
				Name:        item.Name,
				Description: desc,
				Source:      "browse-sh",
				Identifier:  "browse-sh/" + item.Slug,
				TrustLevel:  "community",
				Tags:        item.Tags,
				Extra: map[string]interface{}{
					"slug":               item.Slug,
					"hostname":           item.Hostname,
					"category":           item.Category,
					"source_url":         item.SourceURL,
					"recommended_method": item.RecommendedMethod,
					"proxies":            item.Proxies,
					"install_count":      item.InstallCount,
				},
			})
			if len(results) >= limit {
				break
			}
		}
	}
	return results
}

func (s *BrowseShSource) Inspect(identifier string) (*SkillMeta, error) {
	slug := identifier
	if strings.HasPrefix(slug, "browse-sh/") {
		slug = slug[len("browse-sh/"):]
	}

	catalog, err := s.fetchCatalog()
	if err != nil {
		return nil, err
	}

	for _, item := range catalog {
		if item.Slug == slug {
			desc := item.Description
			if desc == "" {
				desc = item.Title
			}
			return &SkillMeta{
				Name:        item.Name,
				Description: desc,
				Source:      "browse-sh",
				Identifier:  identifier,
				TrustLevel:  "community",
				Tags:        item.Tags,
				Extra: map[string]interface{}{
					"slug":               item.Slug,
					"hostname":           item.Hostname,
					"category":           item.Category,
					"source_url":         item.SourceURL,
					"recommended_method": item.RecommendedMethod,
					"proxies":            item.Proxies,
					"install_count":      item.InstallCount,
				},
			}, nil
		}
	}

	return nil, fmt.Errorf("browse-sh skill not found: %s", slug)
}

func (s *BrowseShSource) Fetch(identifier string) (*SkillBundle, error) {
	meta, err := s.Inspect(identifier)
	if err != nil {
		return nil, err
	}

	slug := meta.Name
	if extra, ok := meta.Extra["slug"].(string); ok {
		slug = extra
	}

	// Resolve SKILL.md content
	detailURL := fmt.Sprintf("https://browse.sh/api/skills/%s", slug)
	body, code, err := guardedHttpGet(detailURL, nil)
	if err != nil || code != 200 {
		return nil, fmt.Errorf("failed to fetch browse-sh detail: %v", err)
	}

	var detail struct {
		SkillMdURL string `json:"skillMdUrl"`
	}
	if err := json.Unmarshal(body, &detail); err != nil {
		return nil, err
	}

	mdURL := detail.SkillMdURL
	if mdURL == "" {
		if sourceURL, ok := meta.Extra["source_url"].(string); ok && strings.Contains(sourceURL, "raw.githubusercontent.com") {
			mdURL = sourceURL
		}
	}

	if mdURL == "" {
		return nil, fmt.Errorf("could not resolve SKILL.md URL for browse-sh")
	}

	mdBody, code, err := guardedHttpGet(mdURL, nil)
	if err != nil || code != 200 {
		return nil, fmt.Errorf("failed to download SKILL.md from %s: %v", mdURL, err)
	}

	files := map[string][]byte{
		"SKILL.md": mdBody,
	}

	return &SkillBundle{
		Name:       meta.Name,
		Files:      files,
		Source:     "browse-sh",
		Identifier: identifier,
		TrustLevel: "community",
		Metadata:   meta.Extra,
	}, nil
}

// ---------------------------------------------------------------------------
// Centralized Hermes Index Source Adapter
// ---------------------------------------------------------------------------

type HermesIndexSource struct {
	auth    GitHubAuth
	index   *hermesCentralIndex
	loaded  bool
	github  *GitHubSource
	indexMu sync.Mutex
}

type hermesCentralIndex struct {
	Skills []hermesCentralIndexEntry `json:"skills"`
}

type hermesCentralIndexEntry struct {
	Name             string                 `json:"name"`
	Description      string                 `json:"description"`
	Source           string                 `json:"source"`
	Identifier       string                 `json:"identifier"`
	TrustLevel       string                 `json:"trust_level"`
	Repo             string                 `json:"repo,omitempty"`
	Path             string                 `json:"path,omitempty"`
	ResolvedGitHubID string                 `json:"resolved_github_id,omitempty"`
	Tags             []string               `json:"tags"`
	Extra            map[string]interface{} `json:"extra"`
}

func (s *HermesIndexSource) SourceID() string {
	return "hermes-index"
}

func (s *HermesIndexSource) TrustLevelFor(identifier string) string {
	_ = s.ensureLoaded()
	if s.index != nil {
		for _, sk := range s.index.Skills {
			if sk.Identifier == identifier {
				return sk.TrustLevel
			}
		}
	}
	return "community"
}

func (s *HermesIndexSource) Search(query string, limit int) []SkillMeta {
	_ = s.ensureLoaded()
	if s.index == nil {
		return nil
	}

	queryLower := strings.ToLower(query)
	var results []SkillMeta
	for _, sk := range s.index.Skills {
		searchable := strings.ToLower(sk.Name + " " + sk.Description + " " + strings.Join(sk.Tags, " "))
		if queryLower == "" || strings.Contains(searchable, queryLower) {
			results = append(results, s.toMeta(sk))
			if limit > 0 && len(results) >= limit {
				break
			}
		}
	}
	return results
}

func (s *HermesIndexSource) Inspect(identifier string) (*SkillMeta, error) {
	_ = s.ensureLoaded()
	if s.index == nil {
		return nil, fmt.Errorf("hermes central index not loaded")
	}

	entry := s.findEntry(identifier)
	if entry == nil {
		return nil, fmt.Errorf("skill not found in hermes index: %s", identifier)
	}

	meta := s.toMeta(*entry)
	return &meta, nil
}

func (s *HermesIndexSource) Fetch(identifier string) (*SkillBundle, error) {
	_ = s.ensureLoaded()
	if s.index == nil {
		return nil, fmt.Errorf("hermes central index not loaded")
	}

	entry := s.findEntry(identifier)
	if entry == nil {
		return nil, fmt.Errorf("skill not found in hermes index: %s", identifier)
	}

	if s.github == nil {
		s.github = &GitHubSource{auth: s.auth}
	}

	target := entry.ResolvedGitHubID
	if target == "" && entry.Repo != "" && entry.Path != "" {
		target = entry.Repo + "/" + entry.Path
	}

	if target == "" {
		return nil, fmt.Errorf("could not resolve github ID for fetch")
	}

	bundle, err := s.github.Fetch(target)
	if err == nil && bundle != nil {
		bundle.Source = entry.Source
		bundle.Identifier = identifier
	}
	return bundle, err
}

func (s *HermesIndexSource) ensureLoaded() error {
	s.indexMu.Lock()
	defer s.indexMu.Unlock()
	if s.loaded {
		return nil
	}

	cacheKey := "hermes-index"
	if cached, ok := readIndexCache(cacheKey); ok {
		var idx hermesCentralIndex
		if err := json.Unmarshal(cached, &idx); err == nil {
			s.index = &idx
			s.loaded = true
			return nil
		}
	}

	// Fetch from nousresearch
	body, code, err := guardedHttpGet("https://hermes-agent.nousresearch.com/docs/api/skills-index.json", nil)
	if err != nil || code != 200 {
		return fmt.Errorf("failed to fetch central index: %v", err)
	}

	var idx hermesCentralIndex
	if err := json.Unmarshal(body, &idx); err != nil {
		return err
	}

	s.index = &idx
	s.loaded = true
	writeIndexCache(cacheKey, body)
	return nil
}

func (s *HermesIndexSource) findEntry(identifier string) *hermesCentralIndexEntry {
	if s.index == nil {
		return nil
	}
	for _, sk := range s.index.Skills {
		if sk.Identifier == identifier {
			return &sk
		}
	}

	// normalize identifier and try matching
	normalized := identifier
	prefixes := []string{"skills-sh/", "skills.sh/", "official/", "github/", "clawhub/"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(identifier, prefix) {
			normalized = identifier[len(prefix):]
			break
		}
	}

	for _, sk := range s.index.Skills {
		stored := sk.Identifier
		for _, prefix := range prefixes {
			if strings.HasPrefix(stored, prefix) {
				stored = stored[len(prefix):]
				break
			}
		}
		if stored == normalized {
			return &sk
		}
	}

	return nil
}

func (s *HermesIndexSource) toMeta(entry hermesCentralIndexEntry) SkillMeta {
	return SkillMeta{
		Name:        entry.Name,
		Description: entry.Description,
		Source:      entry.Source,
		Identifier:  entry.Identifier,
		TrustLevel:  entry.TrustLevel,
		Repo:        entry.Repo,
		Path:        entry.Path,
		Tags:        entry.Tags,
		Extra:       entry.Extra,
	}
}

// ---------------------------------------------------------------------------
// Helper: MD5 Hash
// ---------------------------------------------------------------------------

func md5Hash(val string) string {
	h := md5.Sum([]byte(val))
	return fmt.Sprintf("%x", h)
}
