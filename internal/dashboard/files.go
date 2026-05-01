package dashboard

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DashboardRef identifies a dashboard file served by the local dashboard server.
type DashboardRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Order int    `json:"order,omitempty"`
	Path  string `json:"-"`
}

// DiscoverDashboardFiles finds and validates one dashboard file or a directory of dashboards.
func DiscoverDashboardFiles(path string) ([]DashboardRef, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat dashboard path: %w", err)
	}
	if !info.IsDir() {
		ref, err := dashboardRefForFile(filepath.Base(path), path)
		if err != nil {
			return nil, err
		}
		return []DashboardRef{ref}, nil
	}

	var refs []DashboardRef
	err = filepath.WalkDir(path, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") && filePath != path {
				return filepath.SkipDir
			}
			return nil
		}
		if !isDashboardYAML(filePath) {
			return nil
		}
		rel, err := filepath.Rel(path, filePath)
		if err != nil {
			return fmt.Errorf("dashboard relative path: %w", err)
		}
		ref, err := dashboardRefForFile(filepath.ToSlash(rel), filePath)
		if err != nil {
			return err
		}
		refs = append(refs, ref)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover dashboards: %w", err)
	}
	sort.Slice(refs, func(i, j int) bool {
		leftOrdered := refs[i].Order > 0
		rightOrdered := refs[j].Order > 0
		if leftOrdered != rightOrdered {
			return leftOrdered
		}
		if leftOrdered && refs[i].Order != refs[j].Order {
			return refs[i].Order < refs[j].Order
		}
		return refs[i].ID < refs[j].ID
	})
	if len(refs) == 0 {
		return nil, fmt.Errorf("no dashboard .yaml or .yml files found in %s", path)
	}
	return refs, nil
}

func dashboardRefForFile(id, path string) (DashboardRef, error) {
	d, _, err := LoadFile(path)
	if err != nil {
		return DashboardRef{}, err
	}
	if err = Validate(d); err != nil {
		return DashboardRef{}, err
	}
	return DashboardRef{ID: id, Title: d.Title, Order: d.Order, Path: path}, nil
}

func isDashboardYAML(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}
