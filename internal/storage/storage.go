package storage

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vpngen/embassy-tgbot-admin/internal/model"
)

// FileStore persists flows as individual JSON files in a directory.
// It also maintains in-memory drafts that are flushed to disk on publish.
type FileStore struct {
	mu  sync.RWMutex
	dir string

	// In-memory drafts (not yet published to disk).
	// Keys: "main", "main_en" for flows; "", "en" for decisions/ministry.
	draftFlows     map[string]*model.Flow
	draftDecisions map[string]*model.DecisionConfig
	draftMinistry  map[string]*model.MinistryMessages
}

// NewFileStore creates a FileStore backed by the given directory.
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create flows dir: %w", err)
	}
	return &FileStore{
		dir:            dir,
		draftFlows:     make(map[string]*model.Flow),
		draftDecisions: make(map[string]*model.DecisionConfig),
		draftMinistry:  make(map[string]*model.MinistryMessages),
	}, nil
}

// langSuffix returns the file-name suffix for the given language.
// "" or "ru" → "", "en" → "_en".
func langSuffix(lang string) string {
	if lang == "en" {
		return "_en"
	}
	return ""
}

// List returns summary info for every stored flow.
func (fs *FileStore) List() ([]model.FlowListItem, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	entries, err := os.ReadDir(fs.dir)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}

	var items []model.FlowListItem
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}

		// Skip language-specific files (e.g. main_en.json) and decisions/ministry files.
		name := strings.TrimSuffix(e.Name(), ".json")
		if strings.HasSuffix(name, "_en") || strings.HasPrefix(name, "decisions") || strings.HasPrefix(name, "ministry") {
			continue
		}

		flow, err := fs.readFile(filepath.Join(fs.dir, e.Name()))
		if err != nil {
			continue
		}

		items = append(items, model.FlowListItem{
			ID:         flow.ID,
			Name:       flow.Name,
			StageCount: len(flow.Stages),
			UpdatedAt:  flow.UpdatedAt,
		})
	}

	return items, nil
}

// Get loads a single flow by ID.
func (fs *FileStore) Get(id string) (*model.Flow, error) {
	return fs.GetLang(id, "")
}

// GetLang loads a single flow by ID and language.
func (fs *FileStore) GetLang(id, lang string) (*model.Flow, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	path := filepath.Join(fs.dir, id+langSuffix(lang)+".json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("flow %q (lang=%s) not found", id, lang)
	}

	return fs.readFile(path)
}

// Save writes a flow to disk, updating the timestamp.
func (fs *FileStore) Save(flow *model.Flow) error {
	return fs.SaveLang(flow, "")
}

// SaveLang writes a flow to disk for the given language.
func (fs *FileStore) SaveLang(flow *model.Flow, lang string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	flow.UpdatedAt = time.Now().UTC()

	data, err := json.MarshalIndent(flow, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal flow: %w", err)
	}

	path := filepath.Join(fs.dir, flow.ID+langSuffix(lang)+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write flow: %w", err)
	}

	return nil
}

// Delete removes a flow from disk.
func (fs *FileStore) Delete(id string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	path := filepath.Join(fs.dir, id+".json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("flow %q not found", id)
	}

	return os.Remove(path)
}

func (fs *FileStore) readFile(path string) (*model.Flow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var flow model.Flow
	if err := json.Unmarshal(data, &flow); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", path, err)
	}

	return &flow, nil
}

// WriteZip archives every file in the flows directory into a zip written to w.
func (fs *FileStore) WriteZip(w io.Writer) error {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	entries, err := os.ReadDir(fs.dir)
	if err != nil {
		return fmt.Errorf("read dir: %w", err)
	}

	zw := zip.NewWriter(w)

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		if err := writeZipEntry(zw, fs.dir, e.Name()); err != nil {
			return err
		}
	}

	return zw.Close()
}

func writeZipEntry(zw *zip.Writer, dir, name string) error {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return fmt.Errorf("read %s: %w", name, err)
	}

	fw, err := zw.Create(name)
	if err != nil {
		return fmt.Errorf("create zip entry %s: %w", name, err)
	}

	if _, err := fw.Write(data); err != nil {
		return fmt.Errorf("write zip entry %s: %w", name, err)
	}

	return nil
}

// GetDecisions loads the decisions config from decisions.json.
func (fs *FileStore) GetDecisions() (*model.DecisionConfig, error) {
	return fs.GetDecisionsLang("")
}

// GetDecisionsLang loads the decisions config for the given language.
func (fs *FileStore) GetDecisionsLang(lang string) (*model.DecisionConfig, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	path := filepath.Join(fs.dir, "decisions"+langSuffix(lang)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("decisions%s.json not found", langSuffix(lang))
		}
		return nil, fmt.Errorf("read decisions: %w", err)
	}

	var cfg model.DecisionConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal decisions: %w", err)
	}

	return &cfg, nil
}

// SaveDecisions writes the decisions config to decisions.json.
func (fs *FileStore) SaveDecisions(cfg *model.DecisionConfig) error {
	return fs.SaveDecisionsLang(cfg, "")
}

// SaveDecisionsLang writes the decisions config for the given language.
func (fs *FileStore) SaveDecisionsLang(cfg *model.DecisionConfig, lang string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	cfg.UpdatedAt = time.Now().UTC()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal decisions: %w", err)
	}

	path := filepath.Join(fs.dir, "decisions"+langSuffix(lang)+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write decisions: %w", err)
	}

	return nil
}

// GetMinistryMessages loads the ministry messages for the given language.
func (fs *FileStore) GetMinistryMessages(lang string) (*model.MinistryMessages, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	path := filepath.Join(fs.dir, "ministry"+langSuffix(lang)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("ministry%s.json not found", langSuffix(lang))
		}
		return nil, fmt.Errorf("read ministry messages: %w", err)
	}

	var cfg model.MinistryMessages
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal ministry messages: %w", err)
	}

	return &cfg, nil
}

// SaveMinistryMessages writes the ministry messages for the given language.
func (fs *FileStore) SaveMinistryMessages(cfg *model.MinistryMessages, lang string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	cfg.UpdatedAt = time.Now().UTC()

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal ministry messages: %w", err)
	}

	path := filepath.Join(fs.dir, "ministry"+langSuffix(lang)+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write ministry messages: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Draft layer: in-memory staging before publish.
// ---------------------------------------------------------------------------

// SaveFlowDraft stages a flow edit in memory without writing to disk.
func (fs *FileStore) SaveFlowDraft(flow *model.Flow, lang string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	flow.UpdatedAt = time.Now().UTC()
	key := flow.ID + langSuffix(lang)
	fs.draftFlows[key] = flow
}

// GetFlowDraft returns the draft for a flow, or nil if none exists.
func (fs *FileStore) GetFlowDraft(id, lang string) *model.Flow {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	return fs.draftFlows[id+langSuffix(lang)]
}

// SaveDecisionsDraft stages a decisions edit in memory.
func (fs *FileStore) SaveDecisionsDraft(cfg *model.DecisionConfig, lang string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	cfg.UpdatedAt = time.Now().UTC()
	fs.draftDecisions[lang] = cfg
}

// GetDecisionsDraft returns the draft for decisions, or nil if none exists.
func (fs *FileStore) GetDecisionsDraft(lang string) *model.DecisionConfig {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	return fs.draftDecisions[lang]
}

// SaveMinistryDraft stages a ministry messages edit in memory.
func (fs *FileStore) SaveMinistryDraft(cfg *model.MinistryMessages, lang string) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	cfg.UpdatedAt = time.Now().UTC()
	fs.draftMinistry[lang] = cfg
}

// GetMinistryDraft returns the draft for ministry messages, or nil if none exists.
func (fs *FileStore) GetMinistryDraft(lang string) *model.MinistryMessages {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	return fs.draftMinistry[lang]
}

// DraftItem describes a single pending draft change.
type DraftItem struct {
	Kind      string    `json:"kind"` // "flow", "decisions", "ministry"
	ID        string    `json:"id"`   // flow ID, or "decisions" / "ministry"
	Lang      string    `json:"lang"` // "ru" or "en"
	UpdatedAt time.Time `json:"updated_at"`
}

// PendingDrafts returns a summary of all staged changes.
func (fs *FileStore) PendingDrafts() []DraftItem {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	var items []DraftItem
	for key, f := range fs.draftFlows {
		id := key
		lang := "ru"
		if strings.HasSuffix(key, "_en") {
			id = strings.TrimSuffix(key, "_en")
			lang = "en"
		}
		items = append(items, DraftItem{Kind: "flow", ID: id, Lang: lang, UpdatedAt: f.UpdatedAt})
	}
	for lang, d := range fs.draftDecisions {
		l := "ru"
		if lang == "en" {
			l = "en"
		}
		items = append(items, DraftItem{Kind: "decisions", ID: "decisions", Lang: l, UpdatedAt: d.UpdatedAt})
	}
	for lang, m := range fs.draftMinistry {
		l := "ru"
		if lang == "en" {
			l = "en"
		}
		items = append(items, DraftItem{Kind: "ministry", ID: "ministry", Lang: l, UpdatedAt: m.UpdatedAt})
	}
	return items
}

// PublishAll flushes all pending drafts to disk. Returns the list of files written.
func (fs *FileStore) PublishAll() ([]string, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	var files []string

	for key, flow := range fs.draftFlows {
		data, err := json.MarshalIndent(flow, "", "  ")
		if err != nil {
			return files, fmt.Errorf("marshal flow %s: %w", key, err)
		}
		path := filepath.Join(fs.dir, key+".json")
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return files, fmt.Errorf("write flow %s: %w", key, err)
		}
		files = append(files, key+".json")
	}

	for lang, cfg := range fs.draftDecisions {
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return files, fmt.Errorf("marshal decisions: %w", err)
		}
		name := "decisions" + langSuffix(lang) + ".json"
		path := filepath.Join(fs.dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return files, fmt.Errorf("write decisions: %w", err)
		}
		files = append(files, name)
	}

	for lang, cfg := range fs.draftMinistry {
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return files, fmt.Errorf("marshal ministry: %w", err)
		}
		name := "ministry" + langSuffix(lang) + ".json"
		path := filepath.Join(fs.dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return files, fmt.Errorf("write ministry: %w", err)
		}
		files = append(files, name)
	}

	// Clear all drafts after successful publish.
	fs.draftFlows = make(map[string]*model.Flow)
	fs.draftDecisions = make(map[string]*model.DecisionConfig)
	fs.draftMinistry = make(map[string]*model.MinistryMessages)

	return files, nil
}

// DiscardDrafts clears all pending drafts without writing to disk.
func (fs *FileStore) DiscardDrafts() {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	fs.draftFlows = make(map[string]*model.Flow)
	fs.draftDecisions = make(map[string]*model.DecisionConfig)
	fs.draftMinistry = make(map[string]*model.MinistryMessages)
}
