package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vpngen/embassy-tgbot-admin/internal/model"
)

// FileStore persists flows as individual JSON files in a directory.
type FileStore struct {
	mu  sync.RWMutex
	dir string
}

// NewFileStore creates a FileStore backed by the given directory.
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create flows dir: %w", err)
	}
	return &FileStore{dir: dir}, nil
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
