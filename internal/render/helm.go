package render

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/GreedyKomodoDragon/cabure/api/v1alpha1"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
)

type HelmRenderer struct{}

func (r HelmRenderer) Render(ctx context.Context, checkoutRoot string, spec v1alpha1.GitApplicationSpec, resolvedRevision string) ([]*unstructured.Unstructured, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_ = resolvedRevision
	appRoot, err := ResolveWithinRoot(checkoutRoot, spec.Source.Path)
	if err != nil {
		return nil, err
	}
	if spec.Render.Helm == nil {
		return nil, fmt.Errorf("helm render configuration is required")
	}
	ch, err := loader.Load(appRoot)
	if err != nil {
		return nil, fmt.Errorf("load chart: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	values, err := readHelmValues(ctx, appRoot, spec.Render.Helm.ValuesFiles)
	if err != nil {
		return nil, err
	}
	options := chartutil.ReleaseOptions{
		Name:      spec.Render.Helm.ReleaseName,
		Namespace: spec.Destination.Namespace,
		Revision:  1,
		IsInstall: true,
	}
	renderVals, err := chartutil.ToRenderValues(ch, values, options, chartutil.DefaultCapabilities)
	if err != nil {
		return nil, fmt.Errorf("prepare render values: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	manifests, err := engine.Render(ch, renderVals)
	if err != nil {
		return nil, fmt.Errorf("render chart: %w", err)
	}
	var out []*unstructured.Unstructured
	if spec.Render.Helm.IncludeCRDs {
		crds := ch.CRDObjects()
		sort.Slice(crds, func(i, j int) bool {
			return crds[i].Filename < crds[j].Filename
		})
		for _, crd := range crds {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			docObjs, err := decodeManifest(ctx, crd.Filename, string(crd.File.Data))
			if err != nil {
				return nil, err
			}
			out = append(out, docObjs...)
		}
	}
	keys := make([]string, 0, len(manifests))
	for name := range manifests {
		if strings.HasPrefix(name, "templates/NOTES.txt") {
			continue
		}
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		docObjs, err := decodeManifest(ctx, name, manifests[name])
		if err != nil {
			return nil, err
		}
		out = append(out, docObjs...)
	}
	return out, nil
}

func readHelmValues(ctx context.Context, appRoot string, files []string) (chartutil.Values, error) {
	merged := chartutil.Values{}
	for _, rel := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path, err := ResolveWithinRoot(appRoot, rel)
		if err != nil {
			return nil, err
		}
		content, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return nil, err
		}
		values, err := readValuesDocument(content)
		if err != nil {
			return nil, fmt.Errorf("parse values file %s: %w", rel, err)
		}
		deepMerge(merged, chartutil.Values(values))
	}
	return merged, nil
}

func readValuesDocument(content []byte) (map[string]any, error) {
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(content), 4096)
	out := map[string]any{}
	if err := decoder.Decode(&out); err != nil {
		if errors.Is(err, io.EOF) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	return out, nil
}

func deepMerge(dst chartutil.Values, src chartutil.Values) {
	for key, value := range src {
		existing, ok := dst[key]
		if !ok {
			dst[key] = value
			continue
		}
		dstMap, dstOK := existing.(map[string]any)
		srcMap, srcOK := value.(map[string]any)
		if dstOK && srcOK {
			merged := chartutil.Values(dstMap)
			deepMerge(merged, chartutil.Values(srcMap))
			dst[key] = merged
			continue
		}
		dst[key] = value
	}
}

func decodeManifest(ctx context.Context, source, manifest string) ([]*unstructured.Unstructured, error) {
	reader := utilyaml.NewYAMLReader(bufio.NewReader(strings.NewReader(manifest)))
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
			return nil, fmt.Errorf("%s document %d: %w", source, docIndex+1, err)
		}
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}
		jsonBytes, err := utilyaml.ToJSON(doc)
		if err != nil {
			return nil, fmt.Errorf("%s document %d: %w", source, docIndex+1, err)
		}
		var raw map[string]any
		if err := json.Unmarshal(jsonBytes, &raw); err != nil {
			return nil, fmt.Errorf("%s document %d: %w", source, docIndex+1, err)
		}
		out = append(out, &unstructured.Unstructured{Object: raw})
	}
	return out, nil
}
