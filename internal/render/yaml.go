package render

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/GreedyKomodoDragon/cabure/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
)

type YAMLRenderer struct{}

func (r YAMLRenderer) Render(ctx context.Context, checkoutRoot string, spec v1alpha1.GitApplicationSpec, resolvedRevision string) ([]*unstructured.Unstructured, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_ = resolvedRevision
	appRoot, err := ResolveWithinRoot(checkoutRoot, spec.Source.Path)
	if err != nil {
		return nil, err
	}
	paths, err := yamlFiles(ctx, appRoot)
	if err != nil {
		return nil, err
	}
	var out []*unstructured.Unstructured
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		objs, err := renderYAMLFile(ctx, path)
		if err != nil {
			return nil, err
		}
		out = append(out, objs...)
	}
	return out, nil
}

func yamlFiles(ctx context.Context, root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink: %s", path)
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".yaml", ".yml":
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func renderYAMLFile(ctx context.Context, path string) ([]*unstructured.Unstructured, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(content)))
	var out []*unstructured.Unstructured
	for docIndex := 0; ; docIndex++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		doc, err := reader.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%s document %d: %w", path, docIndex+1, err)
		}
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}
		jsonBytes, err := utilyaml.ToJSON(doc)
		if err != nil {
			return nil, fmt.Errorf("%s document %d: %w", path, docIndex+1, err)
		}
		var raw map[string]any
		if err := json.Unmarshal(jsonBytes, &raw); err != nil {
			return nil, fmt.Errorf("%s document %d: %w", path, docIndex+1, err)
		}
		u := &unstructured.Unstructured{Object: raw}
		out = append(out, u)
	}
	return out, nil
}
